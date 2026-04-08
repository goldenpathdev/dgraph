package owl

// Parser parses OWL ontologies from various serialization formats.
type Parser struct {
	// ProfileValidation controls whether to reject non-OWL2-RL constructs.
	ProfileValidation bool
}

// NewParser creates a Parser with OWL2 RL profile validation enabled.
func NewParser() *Parser {
	return &Parser{ProfileValidation: true}
}

// ParseTurtle parses an OWL ontology from Turtle format.
func (p *Parser) ParseTurtle(input []byte) (*Ontology, error) {
	return p.parseTurtleImpl(input)
}

// ParseTurtleString parses an OWL ontology from a Turtle format string.
func (p *Parser) ParseTurtleString(input string) (*Ontology, error) {
	return p.parseTurtleImpl([]byte(input))
}
