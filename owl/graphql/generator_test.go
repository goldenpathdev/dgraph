package graphql

import (
	"strings"
	"testing"

	"github.com/dgraph-io/dgraph/v25/owl"
)

func buildTestOntology() (*owl.Ontology, *owl.Reasoner) {
	ont := owl.NewOntology()
	ont.Classes["Animal"] = &owl.Class{IRI: "Animal"}
	ont.Classes["Mammal"] = &owl.Class{IRI: "Mammal", SuperClasses: []owl.ClassIRI{"Animal"}}
	ont.Classes["Dog"] = &owl.Class{IRI: "Dog", SuperClasses: []owl.ClassIRI{"Mammal"}}
	ont.Classes["GoldenRetriever"] = &owl.Class{IRI: "GoldenRetriever", SuperClasses: []owl.ClassIRI{"Dog"}}
	ont.Classes["Cat"] = &owl.Class{IRI: "Cat", SuperClasses: []owl.ClassIRI{"Mammal"}}
	ont.Classes["Person"] = &owl.Class{IRI: "Person"}
	ont.Classes["Fish"] = &owl.Class{IRI: "Fish", SuperClasses: []owl.ClassIRI{"Animal"}}
	ont.Classes["FlyingAnimal"] = &owl.Class{IRI: "FlyingAnimal", SuperClasses: []owl.ClassIRI{"Animal"}}
	ont.Classes["FlyingFish"] = &owl.Class{IRI: "FlyingFish", SuperClasses: []owl.ClassIRI{"Fish", "FlyingAnimal"}}

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
	ont.ObjectProperties["hasFriend"] = &owl.ObjectProperty{
		IRI:    "hasFriend",
		Domain: []owl.ClassIRI{"Person"},
		Range:  []owl.ClassIRI{"Person"},
	}
	ont.ObjectProperties["locatedIn"] = &owl.ObjectProperty{
		IRI: "locatedIn",
		Characteristics: owl.PropertyCharacteristics{
			Transitive: true,
		},
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

	r := owl.NewReasoner(ont)
	r.Build()
	return ont, r
}

// P3-T01: SubClassOf → Implements Interface
func TestGenerateImplements(t *testing.T) {
	ont, r := buildTestOntology()
	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	// Animal should be an interface (has subclasses)
	if !strings.Contains(sdl, "interface Animal {") {
		t.Error("Animal should be generated as an interface")
	}

	// Dog should implement Mammal & Animal
	if !strings.Contains(sdl, "type Dog implements") {
		t.Error("Dog should implement interfaces")
	}
	if !strings.Contains(sdl, "Mammal") {
		t.Error("Dog should implement Mammal")
	}
	if !strings.Contains(sdl, "Animal") {
		t.Error("Dog should implement Animal")
	}
}

// P3-T02: Inherited Fields in Implementing Types
func TestGenerateInheritedFields(t *testing.T) {
	ont, r := buildTestOntology()
	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	// Dog type should contain both 'name' (universal) and 'breed' (Dog domain)
	dogSection := extractTypeSection(sdl, "type Dog")
	if dogSection == "" {
		t.Fatal("Dog type not found in SDL")
	}
	if !strings.Contains(dogSection, "name: String") {
		t.Error("Dog should have inherited 'name' field")
	}
	if !strings.Contains(dogSection, "breed: String") {
		t.Error("Dog should have 'breed' field")
	}
	if !strings.Contains(dogSection, "hasOwner: Person") {
		t.Error("Dog should have inherited 'hasOwner' field")
	}
}

// P3-T03: InverseOf → @hasInverse
func TestGenerateHasInverse(t *testing.T) {
	ont, r := buildTestOntology()
	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sdl, `@hasInverse(field: "isOwnerOf")`) {
		t.Error("hasOwner should have @hasInverse directive pointing to isOwnerOf")
	}
	if !strings.Contains(sdl, `@hasInverse(field: "hasOwner")`) {
		t.Error("isOwnerOf should have @hasInverse directive pointing to hasOwner")
	}
}

// P3-T04: FunctionalProperty → Singular Field
func TestGenerateFunctionalSingular(t *testing.T) {
	ont, r := buildTestOntology()
	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	// hasOwner is FunctionalProperty → should be "hasOwner: Person" not "[Person]"
	animalSection := extractTypeSection(sdl, "type Animal")
	if animalSection == "" {
		t.Fatal("Animal type not found")
	}
	if strings.Contains(animalSection, "[Person]") {
		t.Error("hasOwner (functional) should be singular 'Person', not '[Person]'")
	}
	if !strings.Contains(animalSection, "hasOwner: Person") {
		t.Error("hasOwner should be 'hasOwner: Person'")
	}
}

// P3-T05: Non-Functional → List Field
func TestGenerateNonFunctionalList(t *testing.T) {
	ont, r := buildTestOntology()
	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	// hasFriend is not functional → should be "[Person]"
	personSection := extractTypeSection(sdl, "type Person")
	if personSection == "" {
		t.Fatal("Person type not found")
	}
	if !strings.Contains(personSection, "hasFriend: [Person]") {
		t.Errorf("hasFriend should be list type [Person], got section:\n%s", personSection)
	}
}

// P3-T10: Multiple Inheritance (Diamond)
func TestGenerateMultipleInheritance(t *testing.T) {
	ont, r := buildTestOntology()
	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	// FlyingFish subClassOf Fish, FlyingFish subClassOf FlyingAnimal
	flyingFishSection := extractTypeSection(sdl, "type FlyingFish")
	if flyingFishSection == "" {
		t.Fatal("FlyingFish type not found")
	}
	if !strings.Contains(flyingFishSection, "Fish") {
		t.Error("FlyingFish should implement Fish")
	}
	if !strings.Contains(flyingFishSection, "FlyingAnimal") {
		t.Error("FlyingFish should implement FlyingAnimal")
	}
	if !strings.Contains(flyingFishSection, "Animal") {
		t.Error("FlyingFish should implement Animal (transitive)")
	}
}

// Test Person type has no unnecessary fields
func TestGeneratePersonFields(t *testing.T) {
	ont, r := buildTestOntology()
	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	personSection := extractTypeSection(sdl, "type Person")
	if personSection == "" {
		t.Fatal("Person type not found")
	}
	// Person should NOT have 'breed' (Dog domain only)
	if strings.Contains(personSection, "breed") {
		t.Error("Person should not have 'breed' field")
	}
	// Person should have 'isOwnerOf' and 'hasFriend'
	if !strings.Contains(personSection, "isOwnerOf") {
		t.Error("Person should have 'isOwnerOf' field")
	}
}

// Test end-to-end from Turtle
func TestGenerateFromTurtle(t *testing.T) {
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
    rdfs:range :Person ;
    owl:inverseOf :isOwnerOf .
:isOwnerOf a owl:ObjectProperty ;
    rdfs:domain :Person ;
    rdfs:range :Animal .

:name a owl:DatatypeProperty, owl:FunctionalProperty ;
    rdfs:range xsd:string .
`
	ont, err := owl.ParseTurtleString(input)
	if err != nil {
		t.Fatal(err)
	}

	r := owl.NewReasoner(ont)
	r.Build()

	g := NewGenerator(ont, r)
	sdl, err := g.GenerateSDL()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Generated GraphQL SDL:\n%s", sdl)

	if !strings.Contains(sdl, "interface Animal") {
		t.Error("should generate Animal interface")
	}
	if !strings.Contains(sdl, "type Dog implements Animal") {
		t.Error("Dog should implement Animal")
	}
	if !strings.Contains(sdl, "@hasInverse") {
		t.Error("should generate @hasInverse directives")
	}
}

// extractTypeSection extracts the section for a type declaration from SDL.
func extractTypeSection(sdl, prefix string) string {
	idx := strings.Index(sdl, prefix)
	if idx < 0 {
		return ""
	}
	end := strings.Index(sdl[idx:], "}\n")
	if end < 0 {
		return sdl[idx:]
	}
	return sdl[idx : idx+end+2]
}
