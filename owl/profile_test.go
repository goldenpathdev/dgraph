package owl

import (
	"strings"
	"testing"
)

// P0-T09: Reject conflicting property characteristics
func TestProfileConflictingCharacteristics(t *testing.T) {
	ont := NewOntology()
	ont.ObjectProperties["bad"] = &ObjectProperty{
		IRI: "bad",
		Characteristics: PropertyCharacteristics{
			Symmetric:  true,
			Asymmetric: true,
		},
	}

	err := ValidateProfile(ont)
	if err == nil {
		t.Fatal("expected profile validation error for Symmetric+Asymmetric")
	}
	if !strings.Contains(err.Error(), "Symmetric and Asymmetric") {
		t.Errorf("error should mention Symmetric and Asymmetric, got: %s", err.Error())
	}
}

func TestProfileReflexiveIrreflexive(t *testing.T) {
	ont := NewOntology()
	ont.ObjectProperties["bad"] = &ObjectProperty{
		IRI: "bad",
		Characteristics: PropertyCharacteristics{
			Reflexive:   true,
			Irreflexive: true,
		},
	}

	err := ValidateProfile(ont)
	if err == nil {
		t.Fatal("expected profile validation error for Reflexive+Irreflexive")
	}
	if !strings.Contains(err.Error(), "Reflexive and Irreflexive") {
		t.Errorf("error should mention Reflexive and Irreflexive, got: %s", err.Error())
	}
}

func TestProfileCircularSubClass(t *testing.T) {
	ont := NewOntology()
	ont.Classes["A"] = &Class{IRI: "A", SuperClasses: []ClassIRI{"B"}}
	ont.Classes["B"] = &Class{IRI: "B", SuperClasses: []ClassIRI{"A"}}

	err := ValidateProfile(ont)
	if err == nil {
		t.Fatal("expected profile validation error for circular subClassOf")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error should mention circular, got: %s", err.Error())
	}
}

func TestProfileValidOntology(t *testing.T) {
	ont := NewOntology()
	ont.Classes["Animal"] = &Class{IRI: "Animal"}
	ont.Classes["Dog"] = &Class{IRI: "Dog", SuperClasses: []ClassIRI{"Animal"}}
	ont.ObjectProperties["hasOwner"] = &ObjectProperty{
		IRI:    "hasOwner",
		Domain: []ClassIRI{"Animal"},
		Range:  []ClassIRI{"Person"},
		Characteristics: PropertyCharacteristics{
			Functional: true,
		},
	}

	err := ValidateProfile(ont)
	if err != nil {
		t.Errorf("valid ontology should pass profile validation, got: %v", err)
	}

	// Person should have been auto-created
	if _, ok := ont.Classes["Person"]; !ok {
		t.Error("Person class should be auto-created from range reference")
	}
}
