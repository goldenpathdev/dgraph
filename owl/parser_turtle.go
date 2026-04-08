package owl

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Triple represents an RDF triple (subject, predicate, object).
type Triple struct {
	Subject   string
	Predicate string
	Object    string
	IsLiteral bool   // true if Object is a literal value
	Datatype  string // XSD datatype IRI for literals
	Lang      string // language tag for literals
}

// TurtleParser parses Turtle format into RDF triples and builds an OWL Ontology.
type TurtleParser struct {
	input    []byte
	pos      int
	line     int
	col      int
	prefixes map[string]string
	base     string
	blankID  int
	triples  []Triple
}

// parseTurtle is the main entry point: parses Turtle bytes into an Ontology.
func parseTurtle(input []byte) (*Ontology, error) {
	p := &TurtleParser{
		input:    input,
		line:     1,
		col:      1,
		prefixes: make(map[string]string),
	}

	// Standard OWL/RDF/RDFS prefixes as defaults
	p.prefixes["owl"] = "http://www.w3.org/2002/07/owl#"
	p.prefixes["rdf"] = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	p.prefixes["rdfs"] = "http://www.w3.org/2000/01/rdf-schema#"
	p.prefixes["xsd"] = "http://www.w3.org/2001/XMLSchema#"

	if err := p.parseDocument(); err != nil {
		return nil, fmt.Errorf("turtle parse error at line %d, col %d: %w", p.line, p.col, err)
	}

	return buildOntologyFromTriples(p.triples, p.prefixes)
}

func (p *TurtleParser) parseDocument() error {
	for {
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) {
			break
		}
		if err := p.parseStatement(); err != nil {
			return err
		}
	}
	return nil
}

func (p *TurtleParser) parseStatement() error {
	p.skipWhitespaceAndComments()
	if p.pos >= len(p.input) {
		return nil
	}

	// Check for @prefix or @base
	if p.peekString("@prefix") {
		return p.parsePrefixDirective()
	}
	if p.peekString("@base") {
		return p.parseBaseDirective()
	}
	// SPARQL-style PREFIX/BASE (case-insensitive)
	if p.peekStringCI("prefix") {
		return p.parseSPARQLPrefix()
	}
	if p.peekStringCI("base") {
		return p.parseSPARQLBase()
	}

	return p.parseTriples()
}

func (p *TurtleParser) parsePrefixDirective() error {
	p.advance(7) // @prefix
	p.skipWhitespace()
	prefix := p.parsePName()
	// Strip trailing colon — prefix declaration is "foo:" or just ":"
	prefix = strings.TrimSuffix(prefix, ":")
	p.skipWhitespace()
	iri, err := p.parseIRIRef()
	if err != nil {
		return err
	}
	p.skipWhitespace()
	if !p.consume('.') {
		return fmt.Errorf("expected '.' after @prefix directive")
	}
	p.prefixes[prefix] = iri
	return nil
}

func (p *TurtleParser) parseSPARQLPrefix() error {
	// Consume "PREFIX" (case-insensitive)
	for i := 0; i < 6; i++ {
		p.advance(1)
	}
	p.skipWhitespace()
	prefix := p.parsePName()
	prefix = strings.TrimSuffix(prefix, ":")
	p.skipWhitespace()
	iri, err := p.parseIRIRef()
	if err != nil {
		return err
	}
	p.prefixes[prefix] = iri
	// No dot required for SPARQL-style
	return nil
}

func (p *TurtleParser) parseBaseDirective() error {
	p.advance(5) // @base
	p.skipWhitespace()
	iri, err := p.parseIRIRef()
	if err != nil {
		return err
	}
	p.skipWhitespace()
	if !p.consume('.') {
		return fmt.Errorf("expected '.' after @base directive")
	}
	p.base = iri
	return nil
}

func (p *TurtleParser) parseSPARQLBase() error {
	p.advance(4) // BASE
	p.skipWhitespace()
	iri, err := p.parseIRIRef()
	if err != nil {
		return err
	}
	p.base = iri
	return nil
}

func (p *TurtleParser) parseTriples() error {
	subject, err := p.parseSubject()
	if err != nil {
		return err
	}
	p.skipWhitespaceAndComments()
	if err := p.parsePredicateObjectList(subject); err != nil {
		return err
	}
	p.skipWhitespaceAndComments()
	if !p.consume('.') {
		return fmt.Errorf("expected '.' at end of triple statement")
	}
	return nil
}

func (p *TurtleParser) parsePredicateObjectList(subject string) error {
	for {
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) {
			return nil
		}

		predicate, err := p.parsePredicate()
		if err != nil {
			return err
		}

		if err := p.parseObjectList(subject, predicate); err != nil {
			return err
		}

		p.skipWhitespaceAndComments()
		if !p.consume(';') {
			break
		}
		p.skipWhitespaceAndComments()
		// Allow trailing semicolons before '.'
		if p.pos < len(p.input) && p.input[p.pos] == '.' {
			break
		}
	}
	return nil
}

func (p *TurtleParser) parseObjectList(subject, predicate string) error {
	for {
		p.skipWhitespaceAndComments()
		object, isLiteral, datatype, lang, err := p.parseObject()
		if err != nil {
			return err
		}
		p.triples = append(p.triples, Triple{
			Subject:   subject,
			Predicate: predicate,
			Object:    object,
			IsLiteral: isLiteral,
			Datatype:  datatype,
			Lang:      lang,
		})

		p.skipWhitespaceAndComments()
		if !p.consume(',') {
			break
		}
	}
	return nil
}

func (p *TurtleParser) parseSubject() (string, error) {
	p.skipWhitespaceAndComments()
	if p.pos >= len(p.input) {
		return "", fmt.Errorf("unexpected end of input, expected subject")
	}

	ch := p.input[p.pos]
	switch {
	case ch == '<':
		return p.parseIRIRef()
	case ch == '_':
		return p.parseBlankNode()
	case ch == '[':
		return p.parseBlankNodePropertyList()
	case ch == '(':
		return p.parseCollection()
	default:
		return p.parsePrefixedName()
	}
}

func (p *TurtleParser) parsePredicate() (string, error) {
	p.skipWhitespaceAndComments()
	if p.pos >= len(p.input) {
		return "", fmt.Errorf("unexpected end of input, expected predicate")
	}

	// 'a' is shorthand for rdf:type
	if p.input[p.pos] == 'a' {
		next := p.pos + 1
		if next >= len(p.input) || isWhitespace(p.input[next]) {
			p.advance(1)
			return "http://www.w3.org/1999/02/22-rdf-syntax-ns#type", nil
		}
	}

	ch := p.input[p.pos]
	switch {
	case ch == '<':
		return p.parseIRIRef()
	default:
		return p.parsePrefixedName()
	}
}

func (p *TurtleParser) parseObject() (string, bool, string, string, error) {
	p.skipWhitespaceAndComments()
	if p.pos >= len(p.input) {
		return "", false, "", "", fmt.Errorf("unexpected end of input, expected object")
	}

	ch := p.input[p.pos]
	switch {
	case ch == '<':
		iri, err := p.parseIRIRef()
		return iri, false, "", "", err
	case ch == '_':
		bn, err := p.parseBlankNode()
		return bn, false, "", "", err
	case ch == '[':
		bn, err := p.parseBlankNodePropertyList()
		return bn, false, "", "", err
	case ch == '(':
		coll, err := p.parseCollection()
		return coll, false, "", "", err
	case ch == '"' || ch == '\'':
		return p.parseLiteral()
	case ch >= '0' && ch <= '9' || ch == '+' || ch == '-' || ch == '.':
		return p.parseNumericLiteral()
	default:
		// Could be a prefixed name or boolean
		word := p.peekWord()
		if word == "true" || word == "false" {
			p.advance(len(word))
			return word, true, "http://www.w3.org/2001/XMLSchema#boolean", "", nil
		}
		pn, err := p.parsePrefixedName()
		return pn, false, "", "", err
	}
}

func (p *TurtleParser) parseIRIRef() (string, error) {
	if !p.consume('<') {
		return "", fmt.Errorf("expected '<' at start of IRI")
	}
	start := p.pos
	for p.pos < len(p.input) && p.input[p.pos] != '>' {
		p.pos++
		p.col++
	}
	if p.pos >= len(p.input) {
		return "", fmt.Errorf("unterminated IRI reference")
	}
	iri := string(p.input[start:p.pos])
	p.advance(1) // consume '>'

	// Resolve relative IRIs
	if p.base != "" && !strings.Contains(iri, "://") {
		iri = p.base + iri
	}
	return iri, nil
}

func (p *TurtleParser) parsePrefixedName() (string, error) {
	name := p.parsePName()
	p.skipWhitespace()

	// Split on first colon
	parts := strings.SplitN(name, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid prefixed name: %q", name)
	}
	prefix := parts[0]
	local := parts[1]

	ns, ok := p.prefixes[prefix]
	if !ok {
		return "", fmt.Errorf("undefined prefix: %q", prefix)
	}
	return ns + local, nil
}

func (p *TurtleParser) parseBlankNode() (string, error) {
	if p.pos+1 >= len(p.input) || p.input[p.pos+1] != ':' {
		return "", fmt.Errorf("expected '_:' for blank node")
	}
	p.advance(2) // _:
	start := p.pos
	for p.pos < len(p.input) && isNameChar(p.input[p.pos]) {
		p.pos++
		p.col++
	}
	label := string(p.input[start:p.pos])
	return "_:" + label, nil
}

func (p *TurtleParser) parseBlankNodePropertyList() (string, error) {
	p.advance(1) // consume '['
	p.skipWhitespaceAndComments()

	// Empty blank node []
	if p.pos < len(p.input) && p.input[p.pos] == ']' {
		p.advance(1)
		return p.newBlank(), nil
	}

	bn := p.newBlank()
	if err := p.parsePredicateObjectList(bn); err != nil {
		return "", err
	}
	p.skipWhitespaceAndComments()
	if !p.consume(']') {
		return "", fmt.Errorf("expected ']' at end of blank node property list")
	}
	return bn, nil
}

func (p *TurtleParser) parseCollection() (string, error) {
	p.advance(1) // consume '('
	p.skipWhitespaceAndComments()

	if p.pos < len(p.input) && p.input[p.pos] == ')' {
		p.advance(1)
		return "http://www.w3.org/1999/02/22-rdf-syntax-ns#nil", nil
	}

	// Parse collection items into an RDF list
	var items []string
	for {
		p.skipWhitespaceAndComments()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.advance(1)
			break
		}
		obj, isLit, dt, lang, err := p.parseObject()
		if err != nil {
			return "", err
		}
		_ = isLit
		_ = dt
		_ = lang
		items = append(items, obj)
	}

	// Build RDF list structure
	rdfFirst := "http://www.w3.org/1999/02/22-rdf-syntax-ns#first"
	rdfRest := "http://www.w3.org/1999/02/22-rdf-syntax-ns#rest"
	rdfNil := "http://www.w3.org/1999/02/22-rdf-syntax-ns#nil"

	head := p.newBlank()
	current := head
	for i, item := range items {
		p.triples = append(p.triples, Triple{Subject: current, Predicate: rdfFirst, Object: item})
		if i < len(items)-1 {
			next := p.newBlank()
			p.triples = append(p.triples, Triple{Subject: current, Predicate: rdfRest, Object: next})
			current = next
		} else {
			p.triples = append(p.triples, Triple{Subject: current, Predicate: rdfRest, Object: rdfNil})
		}
	}

	return head, nil
}

func (p *TurtleParser) parseLiteral() (string, bool, string, string, error) {
	quote := p.input[p.pos]
	isLong := false

	// Check for long string (""" or ''')
	if p.pos+2 < len(p.input) && p.input[p.pos+1] == quote && p.input[p.pos+2] == quote {
		isLong = true
		p.advance(3)
	} else {
		p.advance(1)
	}

	var buf bytes.Buffer
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == '\\' {
			p.advance(1)
			if p.pos >= len(p.input) {
				return "", false, "", "", fmt.Errorf("unterminated escape in string")
			}
			esc := p.input[p.pos]
			switch esc {
			case 'n':
				buf.WriteByte('\n')
			case 't':
				buf.WriteByte('\t')
			case 'r':
				buf.WriteByte('\r')
			case '\\':
				buf.WriteByte('\\')
			case '"':
				buf.WriteByte('"')
			case '\'':
				buf.WriteByte('\'')
			default:
				buf.WriteByte(esc)
			}
			p.advance(1)
			continue
		}
		if isLong {
			if ch == quote && p.pos+2 < len(p.input) && p.input[p.pos+1] == quote && p.input[p.pos+2] == quote {
				p.advance(3)
				break
			}
		} else {
			if ch == quote {
				p.advance(1)
				break
			}
		}
		if ch == '\n' {
			p.line++
			p.col = 0
		}
		buf.WriteByte(ch)
		p.advance(1)
	}

	value := buf.String()
	datatype := ""
	lang := ""

	// Check for language tag or datatype
	if p.pos < len(p.input) && p.input[p.pos] == '@' {
		p.advance(1)
		start := p.pos
		for p.pos < len(p.input) && (isLetter(p.input[p.pos]) || p.input[p.pos] == '-') {
			p.pos++
			p.col++
		}
		lang = string(p.input[start:p.pos])
	} else if p.pos+1 < len(p.input) && p.input[p.pos] == '^' && p.input[p.pos+1] == '^' {
		p.advance(2)
		dt, err := p.parseIRIOrPrefixed()
		if err != nil {
			return "", false, "", "", err
		}
		datatype = dt
	}

	if datatype == "" && lang == "" {
		datatype = "http://www.w3.org/2001/XMLSchema#string"
	}

	return value, true, datatype, lang, nil
}

func (p *TurtleParser) parseNumericLiteral() (string, bool, string, string, error) {
	start := p.pos
	hasDecimal := false
	hasExponent := false

	if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
		p.pos++
		p.col++
	}
	for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.pos++
		p.col++
	}
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		// Only treat as decimal if followed by a digit or end-of-token
		// (not if followed by whitespace/newline which means end-of-statement)
		if p.pos+1 < len(p.input) && p.input[p.pos+1] >= '0' && p.input[p.pos+1] <= '9' {
			hasDecimal = true
			p.pos++
			p.col++
			for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
				p.pos++
				p.col++
			}
		}
	}
	if p.pos < len(p.input) && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		hasExponent = true
		p.pos++
		p.col++
		if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
			p.pos++
			p.col++
		}
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.pos++
			p.col++
		}
	}

	value := string(p.input[start:p.pos])
	var dt string
	switch {
	case hasExponent:
		dt = "http://www.w3.org/2001/XMLSchema#double"
	case hasDecimal:
		dt = "http://www.w3.org/2001/XMLSchema#decimal"
	default:
		dt = "http://www.w3.org/2001/XMLSchema#integer"
	}

	return value, true, dt, "", nil
}

func (p *TurtleParser) parseIRIOrPrefixed() (string, error) {
	p.skipWhitespace()
	if p.pos < len(p.input) && p.input[p.pos] == '<' {
		return p.parseIRIRef()
	}
	return p.parsePrefixedName()
}

// parsePName reads a prefix:local or just prefix: token
func (p *TurtleParser) parsePName() string {
	var buf bytes.Buffer
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if isWhitespace(ch) || ch == '.' || ch == ';' || ch == ',' || ch == '[' || ch == ']' || ch == '(' || ch == ')' || ch == '{' || ch == '}' {
			break
		}
		buf.WriteByte(ch)
		p.pos++
		p.col++
	}
	return buf.String()
}

func (p *TurtleParser) newBlank() string {
	p.blankID++
	return fmt.Sprintf("_:b%d", p.blankID)
}

// Helper methods

func (p *TurtleParser) advance(n int) {
	for i := 0; i < n && p.pos < len(p.input); i++ {
		if p.input[p.pos] == '\n' {
			p.line++
			p.col = 1
		} else {
			p.col++
		}
		p.pos++
	}
}

func (p *TurtleParser) consume(ch byte) bool {
	if p.pos < len(p.input) && p.input[p.pos] == ch {
		p.advance(1)
		return true
	}
	return false
}

func (p *TurtleParser) peekString(s string) bool {
	if p.pos+len(s) > len(p.input) {
		return false
	}
	return string(p.input[p.pos:p.pos+len(s)]) == s
}

func (p *TurtleParser) peekStringCI(s string) bool {
	if p.pos+len(s) > len(p.input) {
		return false
	}
	return strings.EqualFold(string(p.input[p.pos:p.pos+len(s)]), s)
}

func (p *TurtleParser) peekWord() string {
	i := p.pos
	for i < len(p.input) && isNameChar(p.input[i]) {
		i++
	}
	return string(p.input[p.pos:i])
}

func (p *TurtleParser) skipWhitespace() {
	for p.pos < len(p.input) && isWhitespace(p.input[p.pos]) {
		if p.input[p.pos] == '\n' {
			p.line++
			p.col = 1
		} else {
			p.col++
		}
		p.pos++
	}
}

func (p *TurtleParser) skipWhitespaceAndComments() {
	for {
		p.skipWhitespace()
		if p.pos < len(p.input) && p.input[p.pos] == '#' {
			for p.pos < len(p.input) && p.input[p.pos] != '\n' {
				p.pos++
			}
		} else {
			break
		}
	}
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n'
}

func isNameChar(ch byte) bool {
	r, _ := utf8.DecodeRune([]byte{ch})
	return unicode.IsLetter(r) || unicode.IsDigit(r) || ch == '_' || ch == '-' || ch == '.' || ch == ':'
}

func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

// buildOntologyFromTriples interprets RDF triples as OWL constructs.
func buildOntologyFromTriples(triples []Triple, prefixes map[string]string) (*Ontology, error) {
	ont := NewOntology()

	rdfType := "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"
	rdfsSubClassOf := "http://www.w3.org/2000/01/rdf-schema#subClassOf"
	rdfsDomain := "http://www.w3.org/2000/01/rdf-schema#domain"
	rdfsRange := "http://www.w3.org/2000/01/rdf-schema#range"
	owlClass := "http://www.w3.org/2002/07/owl#Class"
	owlObjectProperty := "http://www.w3.org/2002/07/owl#ObjectProperty"
	owlDatatypeProperty := "http://www.w3.org/2002/07/owl#DatatypeProperty"
	owlFunctionalProperty := "http://www.w3.org/2002/07/owl#FunctionalProperty"
	owlInverseFunctionalProperty := "http://www.w3.org/2002/07/owl#InverseFunctionalProperty"
	owlTransitiveProperty := "http://www.w3.org/2002/07/owl#TransitiveProperty"
	owlSymmetricProperty := "http://www.w3.org/2002/07/owl#SymmetricProperty"
	owlAsymmetricProperty := "http://www.w3.org/2002/07/owl#AsymmetricProperty"
	owlReflexiveProperty := "http://www.w3.org/2002/07/owl#ReflexiveProperty"
	owlIrreflexiveProperty := "http://www.w3.org/2002/07/owl#IrreflexiveProperty"
	owlInverseOf := "http://www.w3.org/2002/07/owl#inverseOf"
	owlEquivalentClass := "http://www.w3.org/2002/07/owl#equivalentClass"
	owlDisjointWith := "http://www.w3.org/2002/07/owl#disjointWith"
	owlUnionOf := "http://www.w3.org/2002/07/owl#unionOf"
	owlIntersectionOf := "http://www.w3.org/2002/07/owl#intersectionOf"
	rdfFirst := "http://www.w3.org/1999/02/22-rdf-syntax-ns#first"
	rdfRest := "http://www.w3.org/1999/02/22-rdf-syntax-ns#rest"
	rdfNil := "http://www.w3.org/1999/02/22-rdf-syntax-ns#nil"
	rdfsLabel := "http://www.w3.org/2000/01/rdf-schema#label"

	// Index triples by subject for fast lookup
	bySubject := make(map[string][]Triple)
	for _, t := range triples {
		bySubject[t.Subject] = append(bySubject[t.Subject], t)
	}

	// Helper to collect RDF list items
	var collectList func(node string) []string
	collectList = func(node string) []string {
		if node == rdfNil {
			return nil
		}
		var items []string
		for _, t := range bySubject[node] {
			if t.Predicate == rdfFirst {
				items = append(items, t.Object)
			}
		}
		for _, t := range bySubject[node] {
			if t.Predicate == rdfRest {
				items = append(items, collectList(t.Object)...)
			}
		}
		return items
	}

	// Helper to get short name from IRI
	shortName := func(iri string) string {
		// Try to find a matching prefix
		for _, ns := range prefixes {
			if strings.HasPrefix(iri, ns) {
				return iri[len(ns):]
			}
		}
		// Fall back to fragment or last path segment
		if idx := strings.LastIndex(iri, "#"); idx >= 0 {
			return iri[idx+1:]
		}
		if idx := strings.LastIndex(iri, "/"); idx >= 0 {
			return iri[idx+1:]
		}
		return iri
	}

	// Ensure class exists
	ensureClass := func(iri string) *Class {
		ciri := ClassIRI(shortName(iri))
		if _, ok := ont.Classes[ciri]; !ok {
			ont.Classes[ciri] = &Class{IRI: ciri}
		}
		return ont.Classes[ciri]
	}

	// Ensure object property exists
	ensureObjProp := func(iri string) *ObjectProperty {
		piri := PropertyIRI(shortName(iri))
		if _, ok := ont.ObjectProperties[piri]; !ok {
			ont.ObjectProperties[piri] = &ObjectProperty{IRI: piri}
		}
		return ont.ObjectProperties[piri]
	}

	// Ensure data property exists
	ensureDataProp := func(iri string) *DataProperty {
		piri := PropertyIRI(shortName(iri))
		if _, ok := ont.DataProperties[piri]; !ok {
			ont.DataProperties[piri] = &DataProperty{IRI: piri}
		}
		return ont.DataProperties[piri]
	}

	// First pass: identify classes and properties by type declarations
	for _, t := range triples {
		if t.Predicate != rdfType {
			continue
		}
		switch t.Object {
		case owlClass:
			ensureClass(t.Subject)
		case owlObjectProperty:
			ensureObjProp(t.Subject)
		case owlDatatypeProperty:
			ensureDataProp(t.Subject)
		case owlFunctionalProperty:
			// Could be object or data property — check if already declared
			sn := shortName(t.Subject)
			if op, ok := ont.ObjectProperties[PropertyIRI(sn)]; ok {
				op.Characteristics.Functional = true
			} else if dp, ok := ont.DataProperties[PropertyIRI(sn)]; ok {
				dp.IsFunctional = true
			} else {
				// Unknown yet — create as object property, may be re-classified
				op := ensureObjProp(t.Subject)
				op.Characteristics.Functional = true
			}
		case owlInverseFunctionalProperty:
			ensureObjProp(t.Subject).Characteristics.InverseFunctional = true
		case owlTransitiveProperty:
			ensureObjProp(t.Subject).Characteristics.Transitive = true
		case owlSymmetricProperty:
			ensureObjProp(t.Subject).Characteristics.Symmetric = true
		case owlAsymmetricProperty:
			ensureObjProp(t.Subject).Characteristics.Asymmetric = true
		case owlReflexiveProperty:
			ensureObjProp(t.Subject).Characteristics.Reflexive = true
		case owlIrreflexiveProperty:
			ensureObjProp(t.Subject).Characteristics.Irreflexive = true
		}
	}

	// Second pass: relationships
	for _, t := range triples {
		sn := shortName(t.Subject)
		on := shortName(t.Object)

		switch t.Predicate {
		case rdfsSubClassOf:
			cls := ensureClass(t.Subject)
			ensureClass(t.Object)
			// Only add non-blank node superclasses directly
			if !strings.HasPrefix(t.Object, "_:") {
				cls.SuperClasses = appendUnique(cls.SuperClasses, ClassIRI(on))
			}

		case rdfsDomain:
			if op, ok := ont.ObjectProperties[PropertyIRI(sn)]; ok {
				op.Domain = appendUnique(op.Domain, ClassIRI(on))
			} else if dp, ok := ont.DataProperties[PropertyIRI(sn)]; ok {
				dp.Domain = appendUnique(dp.Domain, ClassIRI(on))
			}

		case rdfsRange:
			if op, ok := ont.ObjectProperties[PropertyIRI(sn)]; ok {
				op.Range = appendUnique(op.Range, ClassIRI(on))
			} else if dp, ok := ont.DataProperties[PropertyIRI(sn)]; ok {
				dp.Range = DatatypeIRI(t.Object)
			}

		case owlInverseOf:
			op := ensureObjProp(t.Subject)
			ensureObjProp(t.Object)
			op.InverseOf = PropertyIRI(on)
			// Make bidirectional
			inv := ont.ObjectProperties[PropertyIRI(on)]
			if inv.InverseOf == "" {
				inv.InverseOf = PropertyIRI(sn)
			}

		case owlEquivalentClass:
			cls := ensureClass(t.Subject)
			if strings.HasPrefix(t.Object, "_:") {
				// Build class expression from blank node
				expr := buildClassExpression(t.Object, bySubject, shortName)
				cls.EquivalentTo = append(cls.EquivalentTo, expr)
			}

		case owlDisjointWith:
			cls := ensureClass(t.Subject)
			ensureClass(t.Object)
			cls.DisjointWith = appendUnique(cls.DisjointWith, ClassIRI(on))

		case owlUnionOf:
			cls := ensureClass(t.Subject)
			members := collectList(t.Object)
			var operands []ClassExpression
			for _, m := range members {
				operands = append(operands, ClassExpression{Type: ClassExprNamed, Class: ClassIRI(shortName(m))})
			}
			cls.EquivalentTo = append(cls.EquivalentTo, ClassExpression{
				Type:     ClassExprUnion,
				Operands: operands,
			})

		case owlIntersectionOf:
			cls := ensureClass(t.Subject)
			members := collectList(t.Object)
			var operands []ClassExpression
			for _, m := range members {
				if strings.HasPrefix(m, "_:") {
					operands = append(operands, buildClassExpression(m, bySubject, shortName))
				} else {
					operands = append(operands, ClassExpression{Type: ClassExprNamed, Class: ClassIRI(shortName(m))})
				}
			}
			cls.EquivalentTo = append(cls.EquivalentTo, ClassExpression{
				Type:     ClassExprIntersection,
				Operands: operands,
			})

		case rdfsLabel:
			if cls, ok := ont.Classes[ClassIRI(sn)]; ok {
				cls.Label = t.Object
			}
			if op, ok := ont.ObjectProperties[PropertyIRI(sn)]; ok {
				op.Label = t.Object
			}
			if dp, ok := ont.DataProperties[PropertyIRI(sn)]; ok {
				dp.Label = t.Object
			}
		}
	}

	// Fix FunctionalProperty that was declared alongside DatatypeProperty
	// (the first pass may have put it in ObjectProperties by mistake)
	for _, t := range triples {
		if t.Predicate == rdfType && t.Object == owlFunctionalProperty {
			sn := shortName(t.Subject)
			piri := PropertyIRI(sn)
			if dp, ok := ont.DataProperties[piri]; ok {
				dp.IsFunctional = true
				// Remove from ObjectProperties if it was mistakenly added and has no other data
				if op, ok := ont.ObjectProperties[piri]; ok {
					if len(op.Domain) == 0 && len(op.Range) == 0 && op.InverseOf == "" {
						delete(ont.ObjectProperties, piri)
					}
				}
			}
		}
	}

	return ont, nil
}

// buildClassExpression constructs a ClassExpression from a blank node's triples.
func buildClassExpression(bnode string, bySubject map[string][]Triple, shortName func(string) string) ClassExpression {
	owlOnProperty := "http://www.w3.org/2002/07/owl#onProperty"
	owlSomeValuesFrom := "http://www.w3.org/2002/07/owl#someValuesFrom"
	owlAllValuesFrom := "http://www.w3.org/2002/07/owl#allValuesFrom"
	owlHasValue := "http://www.w3.org/2002/07/owl#hasValue"

	ts := bySubject[bnode]
	var prop, someVF, allVF, hasVal string
	for _, t := range ts {
		switch t.Predicate {
		case owlOnProperty:
			prop = t.Object
		case owlSomeValuesFrom:
			someVF = t.Object
		case owlAllValuesFrom:
			allVF = t.Object
		case owlHasValue:
			hasVal = t.Object
		}
	}

	if prop != "" && someVF != "" {
		return ClassExpression{
			Type:     ClassExprSomeValuesFrom,
			Property: PropertyIRI(shortName(prop)),
			Filler:   ClassIRI(shortName(someVF)),
		}
	}
	if prop != "" && allVF != "" {
		return ClassExpression{
			Type:     ClassExprAllValuesFrom,
			Property: PropertyIRI(shortName(prop)),
			Filler:   ClassIRI(shortName(allVF)),
		}
	}
	if prop != "" && hasVal != "" {
		return ClassExpression{
			Type:     ClassExprHasValue,
			Property: PropertyIRI(shortName(prop)),
			Value:    hasVal,
		}
	}

	return ClassExpression{Type: ClassExprNamed}
}

func appendUnique[T comparable](slice []T, item T) []T {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

// ParseTurtleBytes parses Turtle format bytes into an Ontology.
func ParseTurtleBytes(input []byte) (*Ontology, error) {
	return parseTurtle(input)
}

// ParseTurtleString parses a Turtle format string into an Ontology.
func ParseTurtleString(input string) (*Ontology, error) {
	return parseTurtle([]byte(input))
}

// Update the Parser.ParseTurtle method to use the real implementation.
func (p *Parser) parseTurtleImpl(input []byte) (*Ontology, error) {
	ont, err := parseTurtle(input)
	if err != nil {
		return nil, err
	}
	if p.ProfileValidation {
		if err := ValidateProfile(ont); err != nil {
			return nil, err
		}
	}
	return ont, nil
}

