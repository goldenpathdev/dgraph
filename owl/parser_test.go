package owl

import (
	"os"
	"testing"
)

// P0-T01: Parse Simple Class Hierarchy
func TestParseTurtleClassHierarchy(t *testing.T) {
	input := `
@prefix : <http://example.org/animals#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .

:Animal a owl:Class .
:Dog a owl:Class ; rdfs:subClassOf :Animal .
:GoldenRetriever a owl:Class ; rdfs:subClassOf :Dog .
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	if len(ont.Classes) != 3 {
		t.Fatalf("expected 3 classes, got %d: %v", len(ont.Classes), classNames(ont))
	}

	dog := ont.Classes["Dog"]
	if dog == nil {
		t.Fatal("Dog class not found")
	}
	if len(dog.SuperClasses) != 1 || dog.SuperClasses[0] != "Animal" {
		t.Errorf("Dog superclasses = %v, want [Animal]", dog.SuperClasses)
	}

	gr := ont.Classes["GoldenRetriever"]
	if gr == nil {
		t.Fatal("GoldenRetriever class not found")
	}
	if len(gr.SuperClasses) != 1 || gr.SuperClasses[0] != "Dog" {
		t.Errorf("GoldenRetriever superclasses = %v, want [Dog]", gr.SuperClasses)
	}
}

// P0-T02: Parse Properties with Domain/Range
func TestParseTurtleProperties(t *testing.T) {
	input := `
@prefix : <http://example.org/animals#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

:Animal a owl:Class .
:Person a owl:Class .

:hasOwner a owl:ObjectProperty, owl:FunctionalProperty ;
    rdfs:domain :Animal ;
    rdfs:range :Person .

:name a owl:DatatypeProperty ;
    rdfs:range xsd:string .
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	hasOwner := ont.ObjectProperties["hasOwner"]
	if hasOwner == nil {
		t.Fatal("hasOwner property not found")
	}
	if !hasOwner.Characteristics.Functional {
		t.Error("hasOwner should be functional")
	}
	if len(hasOwner.Domain) != 1 || hasOwner.Domain[0] != "Animal" {
		t.Errorf("hasOwner domain = %v, want [Animal]", hasOwner.Domain)
	}
	if len(hasOwner.Range) != 1 || hasOwner.Range[0] != "Person" {
		t.Errorf("hasOwner range = %v, want [Person]", hasOwner.Range)
	}

	name := ont.DataProperties["name"]
	if name == nil {
		t.Fatal("name data property not found")
	}
	if name.Range != "http://www.w3.org/2001/XMLSchema#string" {
		t.Errorf("name range = %v, want xsd:string", name.Range)
	}
}

// P0-T03: Parse Inverse Properties
func TestParseTurtleInverse(t *testing.T) {
	input := `
@prefix : <http://example.org/animals#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .

:hasOwner a owl:ObjectProperty .
:isOwnerOf a owl:ObjectProperty ;
    owl:inverseOf :hasOwner .
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	isOwnerOf := ont.ObjectProperties["isOwnerOf"]
	if isOwnerOf == nil {
		t.Fatal("isOwnerOf not found")
	}
	if isOwnerOf.InverseOf != "hasOwner" {
		t.Errorf("isOwnerOf.InverseOf = %q, want hasOwner", isOwnerOf.InverseOf)
	}

	// Bidirectional
	hasOwner := ont.ObjectProperties["hasOwner"]
	if hasOwner == nil {
		t.Fatal("hasOwner not found")
	}
	if hasOwner.InverseOf != "isOwnerOf" {
		t.Errorf("hasOwner.InverseOf = %q, want isOwnerOf", hasOwner.InverseOf)
	}
}

// P0-T04: Parse Transitive Properties
func TestParseTurtleTransitive(t *testing.T) {
	input := `
@prefix : <http://example.org/geo#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .

:locatedIn a owl:ObjectProperty, owl:TransitiveProperty .
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	locatedIn := ont.ObjectProperties["locatedIn"]
	if locatedIn == nil {
		t.Fatal("locatedIn not found")
	}
	if !locatedIn.Characteristics.Transitive {
		t.Error("locatedIn should be transitive")
	}
}

// P0-T05: Parse Union and Intersection
func TestParseTurtleUnionIntersection(t *testing.T) {
	input := `
@prefix : <http://example.org/animals#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .

:Animal a owl:Class .
:Dog a owl:Class .
:Cat a owl:Class .
:Hamster a owl:Class .
:Person a owl:Class .
:hasOwner a owl:ObjectProperty .

:DomesticAnimal owl:unionOf ( :Dog :Cat :Hamster ) .

:Pet owl:equivalentClass [
    a owl:Restriction ;
    owl:onProperty :hasOwner ;
    owl:someValuesFrom :Person
] .
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	// DomesticAnimal should have a union expression
	da := ont.Classes["DomesticAnimal"]
	if da == nil {
		t.Fatal("DomesticAnimal not found")
	}
	if len(da.EquivalentTo) == 0 {
		t.Fatal("DomesticAnimal should have equivalent expressions")
	}
	union := da.EquivalentTo[0]
	if union.Type != ClassExprUnion {
		t.Errorf("expected union, got type %d", union.Type)
	}
	if len(union.Operands) != 3 {
		t.Errorf("union should have 3 operands, got %d", len(union.Operands))
	}

	// Pet should have an equivalentClass with someValuesFrom
	pet := ont.Classes["Pet"]
	if pet == nil {
		t.Fatal("Pet not found")
	}
	if len(pet.EquivalentTo) == 0 {
		t.Fatal("Pet should have equivalent expressions")
	}
	expr := pet.EquivalentTo[0]
	if expr.Type != ClassExprSomeValuesFrom {
		t.Errorf("Pet equivalent should be someValuesFrom, got type %d", expr.Type)
	}
	if expr.Property != "hasOwner" {
		t.Errorf("restriction property = %q, want hasOwner", expr.Property)
	}
	if expr.Filler != "Person" {
		t.Errorf("restriction filler = %q, want Person", expr.Filler)
	}
}

// P0-T05b: Parse Symmetric Properties
func TestParseTurtleSymmetric(t *testing.T) {
	input := `
@prefix : <http://example.org/people#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .

:friendOf a owl:ObjectProperty, owl:SymmetricProperty .
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	friendOf := ont.ObjectProperties["friendOf"]
	if friendOf == nil {
		t.Fatal("friendOf not found")
	}
	if !friendOf.Characteristics.Symmetric {
		t.Error("friendOf should be symmetric")
	}
}

// P0-T05c: Parse DisjointWith
func TestParseTurtleDisjoint(t *testing.T) {
	input := `
@prefix : <http://example.org/animals#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .

:Dog a owl:Class ;
    owl:disjointWith :Cat .
:Cat a owl:Class .
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	dog := ont.Classes["Dog"]
	if dog == nil {
		t.Fatal("Dog not found")
	}
	if len(dog.DisjointWith) != 1 || dog.DisjointWith[0] != "Cat" {
		t.Errorf("Dog.DisjointWith = %v, want [Cat]", dog.DisjointWith)
	}
}

// Parse the full animals.ttl test ontology
func TestParseAnimalsTTL(t *testing.T) {
	data, err := os.ReadFile("testdata/animals.ttl")
	if err != nil {
		t.Fatalf("failed to read testdata/animals.ttl: %v", err)
	}

	ont, err := ParseTurtleBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	// Verify class count
	expectedClasses := []string{"Animal", "Mammal", "Bird", "Dog", "Cat", "GoldenRetriever",
		"Labrador", "Parrot", "Person", "Place", "Country", "City", "DomesticAnimal"}
	for _, name := range expectedClasses {
		if _, ok := ont.Classes[ClassIRI(name)]; !ok {
			t.Errorf("missing class: %s", name)
		}
	}

	// Verify property count
	expectedObjProps := []string{"hasOwner", "isOwnerOf", "locatedIn", "livesIn", "friendOf"}
	for _, name := range expectedObjProps {
		if _, ok := ont.ObjectProperties[PropertyIRI(name)]; !ok {
			t.Errorf("missing object property: %s", name)
		}
	}

	expectedDataProps := []string{"name", "birthDate", "breed", "weight"}
	for _, name := range expectedDataProps {
		if _, ok := ont.DataProperties[PropertyIRI(name)]; !ok {
			t.Errorf("missing data property: %s", name)
		}
	}

	// Verify specific relationships
	locatedIn := ont.ObjectProperties["locatedIn"]
	if !locatedIn.Characteristics.Transitive {
		t.Error("locatedIn should be transitive")
	}

	hasOwner := ont.ObjectProperties["hasOwner"]
	if !hasOwner.Characteristics.Functional {
		t.Error("hasOwner should be functional")
	}
	if hasOwner.InverseOf != "isOwnerOf" {
		t.Errorf("hasOwner.InverseOf = %q, want isOwnerOf", hasOwner.InverseOf)
	}

	friendOf := ont.ObjectProperties["friendOf"]
	if !friendOf.Characteristics.Symmetric {
		t.Error("friendOf should be symmetric")
	}

	// Verify subsumption chain via reasoner
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}
	if !r.Subsumes("Animal", "GoldenRetriever") {
		t.Error("Animal should subsume GoldenRetriever")
	}
	if !r.Subsumes("Place", "City") {
		t.Error("Place should subsume City")
	}
	if r.Subsumes("Dog", "Cat") {
		t.Error("Dog should not subsume Cat")
	}
}

// Test that Parser with profile validation works
func TestParserWithValidation(t *testing.T) {
	p := NewParser()
	input := `
@prefix : <http://example.org/#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .

:A a owl:Class .
:B a owl:Class ; rdfs:subClassOf :A .
`
	ont, err := p.ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(ont.Classes) != 2 {
		t.Errorf("expected 2 classes, got %d", len(ont.Classes))
	}
}

// Test comments and blank lines
func TestParseTurtleComments(t *testing.T) {
	input := `
# This is a comment
@prefix : <http://example.org/#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .

# Another comment
:Foo a owl:Class .  # inline comment
`
	ont, err := ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ont.Classes["Foo"]; !ok {
		t.Error("Foo class not found")
	}
}

func classNames(ont *Ontology) []string {
	var names []string
	for k := range ont.Classes {
		names = append(names, string(k))
	}
	return names
}
