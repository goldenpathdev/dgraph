package owl

import "testing"

func TestNewOntology(t *testing.T) {
	ont := NewOntology()
	if ont == nil {
		t.Fatal("NewOntology returned nil")
	}
	if ont.Classes == nil {
		t.Fatal("Classes map not initialized")
	}
	if ont.ObjectProperties == nil {
		t.Fatal("ObjectProperties map not initialized")
	}
	if ont.DataProperties == nil {
		t.Fatal("DataProperties map not initialized")
	}
}

func TestOntologyAddClass(t *testing.T) {
	ont := NewOntology()
	ont.Classes["Animal"] = &Class{
		IRI:   "Animal",
		Label: "Animal",
	}
	ont.Classes["Dog"] = &Class{
		IRI:          "Dog",
		Label:        "Dog",
		SuperClasses: []ClassIRI{"Animal"},
	}

	if len(ont.Classes) != 2 {
		t.Fatalf("expected 2 classes, got %d", len(ont.Classes))
	}
	dog := ont.Classes["Dog"]
	if len(dog.SuperClasses) != 1 || dog.SuperClasses[0] != "Animal" {
		t.Fatalf("Dog superclass should be Animal, got %v", dog.SuperClasses)
	}
}
