// Package owl provides OWL2 RL ontology parsing, reasoning, and internal representation
// for OWLGraph, an ontology-aware graph database.
package owl

// ClassIRI is a string identifier for an OWL class.
type ClassIRI string

// PropertyIRI is a string identifier for an OWL property.
type PropertyIRI string

// Class represents an OWL named class.
type Class struct {
	IRI          ClassIRI
	Label        string
	SuperClasses []ClassIRI
	EquivalentTo []ClassExpression
	DisjointWith []ClassIRI
}

// ObjectProperty represents an OWL object property (links two individuals).
type ObjectProperty struct {
	IRI           PropertyIRI
	Label         string
	Domain        []ClassIRI
	Range         []ClassIRI
	InverseOf     PropertyIRI
	SuperProperties []PropertyIRI
	Characteristics PropertyCharacteristics
}

// DataProperty represents an OWL datatype property (links individual to literal).
type DataProperty struct {
	IRI             PropertyIRI
	Label           string
	Domain          []ClassIRI
	Range           DatatypeIRI
	SuperProperties []PropertyIRI
	IsFunctional    bool
}

// DatatypeIRI identifies an XSD or custom datatype.
type DatatypeIRI string

// Common XSD datatypes.
const (
	XSDString   DatatypeIRI = "http://www.w3.org/2001/XMLSchema#string"
	XSDInteger  DatatypeIRI = "http://www.w3.org/2001/XMLSchema#integer"
	XSDFloat    DatatypeIRI = "http://www.w3.org/2001/XMLSchema#float"
	XSDBoolean  DatatypeIRI = "http://www.w3.org/2001/XMLSchema#boolean"
	XSDDate     DatatypeIRI = "http://www.w3.org/2001/XMLSchema#date"
	XSDDateTime DatatypeIRI = "http://www.w3.org/2001/XMLSchema#dateTime"
)

// PropertyCharacteristics holds OWL property characteristic flags.
type PropertyCharacteristics struct {
	Functional        bool
	InverseFunctional bool
	Transitive        bool
	Symmetric         bool
	Asymmetric        bool
	Reflexive         bool
	Irreflexive       bool
}

// ClassExpressionType enumerates the kinds of class expressions.
type ClassExpressionType int

const (
	ClassExprNamed ClassExpressionType = iota
	ClassExprIntersection
	ClassExprUnion
	ClassExprComplement
	ClassExprSomeValuesFrom
	ClassExprAllValuesFrom
	ClassExprHasValue
)

// ClassExpression represents an OWL class expression (named, union, intersection, restriction, etc).
type ClassExpression struct {
	Type       ClassExpressionType
	Class      ClassIRI                // for Named
	Operands   []ClassExpression       // for Union, Intersection
	Complement *ClassExpression        // for Complement
	Property   PropertyIRI            // for Restrictions
	Filler     ClassIRI               // for SomeValuesFrom, AllValuesFrom
	Value      string                 // for HasValue
}

// PropertyChain represents an OWL property chain axiom (e.g., hasGrandparent = hasParent o hasParent).
type PropertyChain struct {
	Property PropertyIRI
	Chain    []PropertyIRI
}

// Ontology is the top-level container for all OWL constructs.
type Ontology struct {
	IRI              string
	Classes          map[ClassIRI]*Class
	ObjectProperties map[PropertyIRI]*ObjectProperty
	DataProperties   map[PropertyIRI]*DataProperty
	PropertyChains   []PropertyChain
}

// NewOntology creates an empty Ontology.
func NewOntology() *Ontology {
	return &Ontology{
		Classes:          make(map[ClassIRI]*Class),
		ObjectProperties: make(map[PropertyIRI]*ObjectProperty),
		DataProperties:   make(map[PropertyIRI]*DataProperty),
	}
}
