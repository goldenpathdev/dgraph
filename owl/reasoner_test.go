package owl

import (
	"testing"
)

// buildTestOntology creates the animal hierarchy for testing:
//
//	Animal > Mammal > Dog > GoldenRetriever
//	                > Cat
//	       > Bird   > Parrot
func buildTestOntology() *Ontology {
	ont := NewOntology()
	ont.Classes["Animal"] = &Class{IRI: "Animal"}
	ont.Classes["Mammal"] = &Class{IRI: "Mammal", SuperClasses: []ClassIRI{"Animal"}}
	ont.Classes["Bird"] = &Class{IRI: "Bird", SuperClasses: []ClassIRI{"Animal"}}
	ont.Classes["Dog"] = &Class{IRI: "Dog", SuperClasses: []ClassIRI{"Mammal"}}
	ont.Classes["Cat"] = &Class{IRI: "Cat", SuperClasses: []ClassIRI{"Mammal"}}
	ont.Classes["GoldenRetriever"] = &Class{IRI: "GoldenRetriever", SuperClasses: []ClassIRI{"Dog"}}
	ont.Classes["Parrot"] = &Class{IRI: "Parrot", SuperClasses: []ClassIRI{"Bird"}}

	ont.ObjectProperties["hasOwner"] = &ObjectProperty{
		IRI:       "hasOwner",
		Domain:    []ClassIRI{"Animal"},
		Range:     []ClassIRI{"Person"},
		InverseOf: "isOwnerOf",
		Characteristics: PropertyCharacteristics{
			Functional: true,
		},
	}
	ont.ObjectProperties["isOwnerOf"] = &ObjectProperty{
		IRI:       "isOwnerOf",
		Domain:    []ClassIRI{"Person"},
		Range:     []ClassIRI{"Animal"},
		InverseOf: "hasOwner",
	}
	ont.ObjectProperties["locatedIn"] = &ObjectProperty{
		IRI: "locatedIn",
		Characteristics: PropertyCharacteristics{
			Transitive: true,
		},
	}
	ont.ObjectProperties["friendOf"] = &ObjectProperty{
		IRI: "friendOf",
		Characteristics: PropertyCharacteristics{
			Symmetric: true,
		},
	}

	return ont
}

// P0-T06: Subsumption Closure
func TestSubsumption(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		a, b ClassIRI
		want bool
	}{
		{"Animal", "GoldenRetriever", true},
		{"Animal", "Dog", true},
		{"Animal", "Parrot", true},
		{"Mammal", "Dog", true},
		{"Dog", "GoldenRetriever", true},
		{"Dog", "Parrot", false},
		{"Cat", "Dog", false},
		{"GoldenRetriever", "Animal", false},
		{"Dog", "Dog", true}, // reflexive
	}

	for _, tt := range tests {
		got := r.Subsumes(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("Subsumes(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// P0-T06: AllSubClasses
func TestAllSubClasses(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	subs := r.AllSubClasses("Animal")
	expected := map[ClassIRI]bool{
		"Mammal": true, "Bird": true, "Dog": true,
		"Cat": true, "GoldenRetriever": true, "Parrot": true,
	}
	if len(subs) != len(expected) {
		t.Fatalf("AllSubClasses(Animal): got %d, want %d: %v", len(subs), len(expected), subs)
	}
	for _, s := range subs {
		if !expected[s] {
			t.Errorf("unexpected subclass: %s", s)
		}
	}
}

// P0-T06: AllSuperClasses
func TestAllSuperClasses(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	supers := r.AllSuperClasses("GoldenRetriever")
	expected := map[ClassIRI]bool{
		"Dog": true, "Mammal": true, "Animal": true,
	}
	if len(supers) != len(expected) {
		t.Fatalf("AllSuperClasses(GoldenRetriever): got %d, want %d: %v", len(supers), len(expected), supers)
	}
	for _, s := range supers {
		if !expected[s] {
			t.Errorf("unexpected superclass: %s", s)
		}
	}
}

// AncestorTypes (used by materializer)
func TestAncestorTypes(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	types := r.AncestorTypes("GoldenRetriever")
	// Should include GoldenRetriever itself + Dog, Mammal, Animal
	if len(types) != 4 {
		t.Fatalf("AncestorTypes(GoldenRetriever): got %d, want 4: %v", len(types), types)
	}
	if types[0] != "GoldenRetriever" {
		t.Errorf("first element should be GoldenRetriever itself, got %s", types[0])
	}
}

// P0-T03: Inverse Properties
func TestInverseProperty(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	inv, ok := r.InverseProperty("hasOwner")
	if !ok || inv != "isOwnerOf" {
		t.Errorf("InverseProperty(hasOwner) = %s, %v; want isOwnerOf, true", inv, ok)
	}

	inv, ok = r.InverseProperty("isOwnerOf")
	if !ok || inv != "hasOwner" {
		t.Errorf("InverseProperty(isOwnerOf) = %s, %v; want hasOwner, true", inv, ok)
	}

	_, ok = r.InverseProperty("locatedIn")
	if ok {
		t.Error("InverseProperty(locatedIn) should return false")
	}
}

// P0-T04: Transitive Properties
func TestIsTransitive(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	if !r.IsTransitive("locatedIn") {
		t.Error("locatedIn should be transitive")
	}
	if r.IsTransitive("hasOwner") {
		t.Error("hasOwner should not be transitive")
	}
}

// P0-T02: Functional Properties
func TestIsFunctional(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	if !r.IsFunctional("hasOwner") {
		t.Error("hasOwner should be functional")
	}
	if r.IsFunctional("locatedIn") {
		t.Error("locatedIn should not be functional")
	}
}

// Symmetric Properties
func TestIsSymmetric(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	if !r.IsSymmetric("friendOf") {
		t.Error("friendOf should be symmetric")
	}
	if r.IsSymmetric("hasOwner") {
		t.Error("hasOwner should not be symmetric")
	}
}

// P0-T08: Domain/Range Inference
func TestInferredTypes(t *testing.T) {
	ont := buildTestOntology()
	r := NewReasoner(ont)
	if err := r.Build(); err != nil {
		t.Fatal(err)
	}

	// Subject of hasOwner should infer Animal
	domain := r.InferredTypes("hasOwner", true)
	if len(domain) != 1 || domain[0] != "Animal" {
		t.Errorf("InferredTypes(hasOwner, subject) = %v, want [Animal]", domain)
	}

	// Object of hasOwner should infer Person
	rng := r.InferredTypes("hasOwner", false)
	if len(rng) != 1 || rng[0] != "Person" {
		t.Errorf("InferredTypes(hasOwner, object) = %v, want [Person]", rng)
	}
}
