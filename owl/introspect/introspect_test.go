package introspect

import (
	"testing"

	"github.com/dgraph-io/dgraph/v25/owl"
)

func buildTestInspector() *Inspector {
	ont := owl.NewOntology()
	ont.Classes["Animal"] = &owl.Class{IRI: "Animal"}
	ont.Classes["Mammal"] = &owl.Class{IRI: "Mammal", SuperClasses: []owl.ClassIRI{"Animal"}}
	ont.Classes["Dog"] = &owl.Class{IRI: "Dog", SuperClasses: []owl.ClassIRI{"Mammal"},
		DisjointWith: []owl.ClassIRI{"Cat"}}
	ont.Classes["Cat"] = &owl.Class{IRI: "Cat", SuperClasses: []owl.ClassIRI{"Mammal"}}
	ont.Classes["GoldenRetriever"] = &owl.Class{IRI: "GoldenRetriever", SuperClasses: []owl.ClassIRI{"Dog"}}
	ont.Classes["Person"] = &owl.Class{IRI: "Person"}

	ont.ObjectProperties["hasOwner"] = &owl.ObjectProperty{
		IRI:       "hasOwner",
		Domain:    []owl.ClassIRI{"Animal"},
		Range:     []owl.ClassIRI{"Person"},
		InverseOf: "isOwnerOf",
		Characteristics: owl.PropertyCharacteristics{Functional: true},
	}
	ont.DataProperties["name"] = &owl.DataProperty{
		IRI: "name", Range: owl.XSDString, IsFunctional: true,
	}
	ont.DataProperties["breed"] = &owl.DataProperty{
		IRI: "breed", Domain: []owl.ClassIRI{"Dog"}, Range: owl.XSDString, IsFunctional: true,
	}

	r := owl.NewReasoner(ont)
	r.Build()
	return NewInspector(ont, r)
}

func TestListClasses(t *testing.T) {
	insp := buildTestInspector()
	classes := insp.ListClasses()
	if len(classes) != 6 {
		t.Errorf("expected 6 classes, got %d", len(classes))
	}
}

func TestGetClass(t *testing.T) {
	insp := buildTestInspector()

	dog := insp.GetClass("Dog")
	if dog == nil {
		t.Fatal("Dog class not found")
	}
	if dog.IRI != "Dog" {
		t.Errorf("IRI = %q, want Dog", dog.IRI)
	}

	// Dog should have subclasses
	if len(dog.SubClasses) == 0 {
		t.Error("Dog should have subclasses (GoldenRetriever)")
	}

	// Dog disjoint with Cat
	if len(dog.DisjointWith) != 1 || dog.DisjointWith[0] != "Cat" {
		t.Errorf("Dog.DisjointWith = %v, want [Cat]", dog.DisjointWith)
	}
}

func TestPropertiesOf(t *testing.T) {
	insp := buildTestInspector()

	// Dog should have: hasOwner (from Animal), name (universal), breed (Dog domain)
	props := insp.PropertiesOf("Dog")
	propNames := make(map[string]bool)
	for _, p := range props {
		propNames[p.IRI] = true
	}

	if !propNames["hasOwner"] {
		t.Error("Dog should have 'hasOwner' (inherited from Animal)")
	}
	if !propNames["name"] {
		t.Error("Dog should have 'name' (universal)")
	}
	if !propNames["breed"] {
		t.Error("Dog should have 'breed' (Dog domain)")
	}

	// Person should NOT have breed
	personProps := insp.PropertiesOf("Person")
	for _, p := range personProps {
		if p.IRI == "breed" {
			t.Error("Person should not have 'breed'")
		}
	}
}

func TestSubClassesOf(t *testing.T) {
	insp := buildTestInspector()

	// Direct subclasses of Animal
	direct := insp.SubClassesOf("Animal", false)
	if len(direct) != 1 || direct[0] != "Mammal" {
		t.Errorf("direct subclasses of Animal = %v, want [Mammal]", direct)
	}

	// Transitive subclasses of Animal: Mammal, Dog, Cat, GoldenRetriever
	transitive := insp.SubClassesOf("Animal", true)
	if len(transitive) != 4 {
		t.Errorf("transitive subclasses of Animal: expected 4, got %d: %v", len(transitive), transitive)
	}
}

func TestSuperClassesOf(t *testing.T) {
	insp := buildTestInspector()

	supers := insp.SuperClassesOf("GoldenRetriever", true)
	expected := map[string]bool{"Dog": true, "Mammal": true, "Animal": true}
	if len(supers) != 3 {
		t.Errorf("expected 3 superclasses, got %d: %v", len(supers), supers)
	}
	for _, s := range supers {
		if !expected[s] {
			t.Errorf("unexpected superclass: %s", s)
		}
	}
}

func TestGetClassNotFound(t *testing.T) {
	insp := buildTestInspector()
	if insp.GetClass("Unknown") != nil {
		t.Error("GetClass should return nil for unknown class")
	}
}
