package compiler

import (
	"testing"

	"github.com/dgraph-io/dgraph/v25/owl"
	pb "github.com/dgraph-io/dgraph/v25/protos/pb"
)

func buildTestOntology() (*owl.Ontology, *owl.Reasoner) {
	ont := owl.NewOntology()
	ont.Classes["Animal"] = &owl.Class{IRI: "Animal"}
	ont.Classes["Dog"] = &owl.Class{IRI: "Dog", SuperClasses: []owl.ClassIRI{"Animal"}}
	ont.Classes["Person"] = &owl.Class{IRI: "Person"}

	ont.ObjectProperties["hasOwner"] = &owl.ObjectProperty{
		IRI:       "hasOwner",
		Domain:    []owl.ClassIRI{"Animal"},
		Range:     []owl.ClassIRI{"Person"},
		InverseOf: "isOwnerOf",
		Characteristics: owl.PropertyCharacteristics{
			Functional: true,
		},
	}
	ont.ObjectProperties["isOwnerOf"] = &owl.ObjectProperty{
		IRI:       "isOwnerOf",
		Domain:    []owl.ClassIRI{"Person"},
		Range:     []owl.ClassIRI{"Animal"},
		InverseOf: "hasOwner",
	}

	ont.DataProperties["name"] = &owl.DataProperty{
		IRI:          "name",
		Range:        owl.XSDString,
		IsFunctional: true,
	}
	ont.DataProperties["breed"] = &owl.DataProperty{
		IRI:          "breed",
		Domain:       []owl.ClassIRI{"Dog"},
		Range:        owl.XSDString,
		IsFunctional: true,
	}
	ont.DataProperties["weight"] = &owl.DataProperty{
		IRI:    "weight",
		Domain: []owl.ClassIRI{"Animal"},
		Range:  owl.XSDFloat,
	}

	r := owl.NewReasoner(ont)
	r.Build()
	return ont, r
}

// P1-T01: OWL Class → Dgraph Type
func TestCompileClassToType(t *testing.T) {
	ont, r := buildTestOntology()
	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	// Find Dog type
	var dogType *pb.TypeUpdate
	for _, tu := range result.Types {
		if tu.TypeName == "Dog" {
			dogType = tu
			break
		}
	}
	if dogType == nil {
		t.Fatal("Dog type not found in compilation result")
	}

	fieldNames := make(map[string]bool)
	for _, f := range dogType.Fields {
		fieldNames[f.Predicate] = true
	}

	// Dog should have: name (universal), breed (Dog domain), hasOwner (Animal domain, inherited), weight (Animal domain, inherited)
	if !fieldNames["name"] {
		t.Error("Dog type should include 'name' field (universal)")
	}
	if !fieldNames["breed"] {
		t.Error("Dog type should include 'breed' field (Dog domain)")
	}
	if !fieldNames["hasOwner"] {
		t.Error("Dog type should include 'hasOwner' field (inherited from Animal)")
	}
	if !fieldNames["weight"] {
		t.Error("Dog type should include 'weight' field (inherited from Animal)")
	}
}

// P1-T02: SubClassOf → Type Includes Parent Fields
func TestCompileInheritedFields(t *testing.T) {
	ont, r := buildTestOntology()
	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	// Animal type should have: name, hasOwner, weight
	var animalType *pb.TypeUpdate
	for _, tu := range result.Types {
		if tu.TypeName == "Animal" {
			animalType = tu
			break
		}
	}
	if animalType == nil {
		t.Fatal("Animal type not found")
	}

	animalFields := make(map[string]bool)
	for _, f := range animalType.Fields {
		animalFields[f.Predicate] = true
	}

	// Dog type should have everything Animal has PLUS breed
	var dogType *pb.TypeUpdate
	for _, tu := range result.Types {
		if tu.TypeName == "Dog" {
			dogType = tu
			break
		}
	}
	if dogType == nil {
		t.Fatal("Dog type not found")
	}

	dogFields := make(map[string]bool)
	for _, f := range dogType.Fields {
		dogFields[f.Predicate] = true
	}

	// Every Animal field should be in Dog
	for field := range animalFields {
		if !dogFields[field] {
			t.Errorf("Dog missing inherited field %q from Animal", field)
		}
	}

	// Dog should also have breed
	if !dogFields["breed"] {
		t.Error("Dog should have 'breed' field")
	}
}

// P1-T03: FunctionalProperty → Non-List Predicate
func TestCompileFunctionalProperty(t *testing.T) {
	ont, r := buildTestOntology()
	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	for _, pred := range result.Predicates {
		if pred.Predicate == "hasOwner" {
			if pred.List {
				t.Error("hasOwner (FunctionalProperty) should have List=false")
			}
			if pred.ValueType != pb.Posting_UID {
				t.Errorf("hasOwner should be UID type, got %v", pred.ValueType)
			}
			return
		}
	}
	t.Error("hasOwner predicate not found")
}

// P1-T04: Non-Functional Property → List Predicate
func TestCompileNonFunctionalProperty(t *testing.T) {
	ont, r := buildTestOntology()
	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	for _, pred := range result.Predicates {
		if pred.Predicate == "isOwnerOf" {
			if !pred.List {
				t.Error("isOwnerOf (non-functional) should have List=true")
			}
			return
		}
	}
	t.Error("isOwnerOf predicate not found")
}

// Test datatype mapping
func TestCompileDatatypeMapping(t *testing.T) {
	ont, r := buildTestOntology()
	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	for _, pred := range result.Predicates {
		switch pred.Predicate {
		case "name":
			if pred.ValueType != pb.Posting_STRING {
				t.Errorf("name should be STRING, got %v", pred.ValueType)
			}
		case "weight":
			if pred.ValueType != pb.Posting_FLOAT {
				t.Errorf("weight should be FLOAT, got %v", pred.ValueType)
			}
		}
	}
}

// Test inverse property gets @reverse
func TestCompileInverseProperty(t *testing.T) {
	ont, r := buildTestOntology()
	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	for _, pred := range result.Predicates {
		if pred.Predicate == "hasOwner" {
			if pred.Directive != pb.SchemaUpdate_REVERSE {
				t.Error("hasOwner with inverseOf should have REVERSE directive")
			}
			return
		}
	}
	t.Error("hasOwner predicate not found")
}

// Test schema string generation
func TestCompileSchemaString(t *testing.T) {
	ont, r := buildTestOntology()
	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	schema := CompileSchemaString(result)
	if schema == "" {
		t.Error("schema string should not be empty")
	}

	// Should contain type definitions
	if !containsStr(schema, "type Dog {") {
		t.Error("schema should contain 'type Dog'")
	}
	if !containsStr(schema, "type Animal {") {
		t.Error("schema should contain 'type Animal'")
	}
	// Should contain predicate definitions
	if !containsStr(schema, "hasOwner: uid") {
		t.Error("schema should contain 'hasOwner: uid'")
	}
	if !containsStr(schema, "name: string") {
		t.Error("schema should contain 'name: string'")
	}
}

// Test end-to-end: parse Turtle → compile → get schema
func TestCompileFromTurtle(t *testing.T) {
	input := `
@prefix : <http://example.org/animals#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

:Animal a owl:Class .
:Dog a owl:Class ; rdfs:subClassOf :Animal .
:Person a owl:Class .

:hasOwner a owl:ObjectProperty, owl:FunctionalProperty ;
    rdfs:domain :Animal ;
    rdfs:range :Person .

:name a owl:DatatypeProperty, owl:FunctionalProperty ;
    rdfs:range xsd:string .

:breed a owl:DatatypeProperty, owl:FunctionalProperty ;
    rdfs:domain :Dog ;
    rdfs:range xsd:string .
`
	ont, err := owl.ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	r := owl.NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	c := NewDgraphCompiler(ont, r)
	result, err := c.Compile()
	if err != nil {
		t.Fatal(err)
	}

	schema := CompileSchemaString(result)
	t.Logf("Generated schema:\n%s", schema)

	if len(result.Types) == 0 {
		t.Error("should have generated types")
	}
	if len(result.Predicates) == 0 {
		t.Error("should have generated predicates")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
