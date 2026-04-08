package materializer

import (
	"testing"

	"github.com/dgraph-io/dgraph/v25/owl"
	pb "github.com/dgraph-io/dgraph/v25/protos/pb"
)

func buildTestReasonerAndMaterializer() *TypeMaterializer {
	ont := owl.NewOntology()
	ont.Classes["Animal"] = &owl.Class{IRI: "Animal"}
	ont.Classes["Mammal"] = &owl.Class{IRI: "Mammal", SuperClasses: []owl.ClassIRI{"Animal"}}
	ont.Classes["Dog"] = &owl.Class{IRI: "Dog", SuperClasses: []owl.ClassIRI{"Mammal"}}
	ont.Classes["GoldenRetriever"] = &owl.Class{IRI: "GoldenRetriever", SuperClasses: []owl.ClassIRI{"Dog"}}
	ont.Classes["Cat"] = &owl.Class{IRI: "Cat", SuperClasses: []owl.ClassIRI{"Mammal"}}
	ont.Classes["Person"] = &owl.Class{IRI: "Person"}

	ont.ObjectProperties["hasOwner"] = &owl.ObjectProperty{
		IRI:       "hasOwner",
		InverseOf: "isOwnerOf",
		Domain:    []owl.ClassIRI{"Animal"},
		Range:     []owl.ClassIRI{"Person"},
	}
	ont.ObjectProperties["isOwnerOf"] = &owl.ObjectProperty{
		IRI:       "isOwnerOf",
		InverseOf: "hasOwner",
	}

	r := owl.NewReasoner(ont)
	r.Build()
	return NewTypeMaterializer(r)
}

// P2-T01: Type Materialization on Write
func TestMaterializeGoldenRetriever(t *testing.T) {
	m := buildTestReasonerAndMaterializer()

	edges := []*pb.DirectedEdge{
		{
			Entity:    0x1,
			Attr:      "dgraph.type",
			Value:     []byte("GoldenRetriever"),
			ValueType: pb.Posting_STRING,
			Op:        pb.DirectedEdge_SET,
		},
	}

	additional := m.MaterializeTypes(edges, "dgraph.type")

	// Should add Dog, Mammal, Animal
	if len(additional) != 3 {
		t.Fatalf("expected 3 materialized type edges, got %d", len(additional))
	}

	types := make(map[string]bool)
	for _, e := range additional {
		types[string(e.Value)] = true
		if e.Entity != 0x1 {
			t.Errorf("materialized edge should have same entity 0x1, got 0x%x", e.Entity)
		}
		if e.Attr != "dgraph.type" {
			t.Errorf("materialized edge attr should be dgraph.type, got %s", e.Attr)
		}
		if e.Op != pb.DirectedEdge_SET {
			t.Errorf("materialized edge op should be SET")
		}
	}

	for _, expected := range []string{"Dog", "Mammal", "Animal"} {
		if !types[expected] {
			t.Errorf("missing materialized type: %s", expected)
		}
	}
}

// Type with no superclass should produce no additional edges
func TestMaterializeLeafType(t *testing.T) {
	m := buildTestReasonerAndMaterializer()

	edges := []*pb.DirectedEdge{
		{
			Entity:    0x1,
			Attr:      "dgraph.type",
			Value:     []byte("Person"),
			ValueType: pb.Posting_STRING,
			Op:        pb.DirectedEdge_SET,
		},
	}

	additional := m.MaterializeTypes(edges, "dgraph.type")
	if len(additional) != 0 {
		t.Fatalf("Person has no superclasses, expected 0 edges, got %d", len(additional))
	}
}

// Unknown type should produce no additional edges
func TestMaterializeUnknownType(t *testing.T) {
	m := buildTestReasonerAndMaterializer()

	edges := []*pb.DirectedEdge{
		{
			Entity:    0x1,
			Attr:      "dgraph.type",
			Value:     []byte("UnknownType"),
			ValueType: pb.Posting_STRING,
			Op:        pb.DirectedEdge_SET,
		},
	}

	additional := m.MaterializeTypes(edges, "dgraph.type")
	if len(additional) != 0 {
		t.Fatalf("unknown type should produce 0 edges, got %d", len(additional))
	}
}

// Non-type edges should be ignored
func TestMaterializeIgnoresNonTypeEdges(t *testing.T) {
	m := buildTestReasonerAndMaterializer()

	edges := []*pb.DirectedEdge{
		{
			Entity:    0x1,
			Attr:      "name",
			Value:     []byte("Fido"),
			ValueType: pb.Posting_STRING,
			Op:        pb.DirectedEdge_SET,
		},
	}

	additional := m.MaterializeTypes(edges, "dgraph.type")
	if len(additional) != 0 {
		t.Fatalf("non-type edges should produce 0 additional edges, got %d", len(additional))
	}
}

// DELETE operations should not trigger materialization
func TestMaterializeIgnoresDelete(t *testing.T) {
	m := buildTestReasonerAndMaterializer()

	edges := []*pb.DirectedEdge{
		{
			Entity:    0x1,
			Attr:      "dgraph.type",
			Value:     []byte("GoldenRetriever"),
			ValueType: pb.Posting_STRING,
			Op:        pb.DirectedEdge_DEL,
		},
	}

	additional := m.MaterializeTypes(edges, "dgraph.type")
	if len(additional) != 0 {
		t.Fatalf("DELETE should not trigger materialization, got %d edges", len(additional))
	}
}

// Multiple type edges in one mutation
func TestMaterializeMultipleEntities(t *testing.T) {
	m := buildTestReasonerAndMaterializer()

	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "dgraph.type", Value: []byte("GoldenRetriever"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
		{Entity: 0x1, Attr: "name", Value: []byte("Fido"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
		{Entity: 0x2, Attr: "dgraph.type", Value: []byte("Cat"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
		{Entity: 0x2, Attr: "name", Value: []byte("Whiskers"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
	}

	additional := m.MaterializeTypes(edges, "dgraph.type")

	// GoldenRetriever → Dog, Mammal, Animal (3)
	// Cat → Mammal, Animal (2)
	// Total: 5
	if len(additional) != 5 {
		t.Fatalf("expected 5 materialized edges, got %d", len(additional))
	}

	entity1Types := make(map[string]bool)
	entity2Types := make(map[string]bool)
	for _, e := range additional {
		if e.Entity == 0x1 {
			entity1Types[string(e.Value)] = true
		} else if e.Entity == 0x2 {
			entity2Types[string(e.Value)] = true
		}
	}

	if !entity1Types["Dog"] || !entity1Types["Mammal"] || !entity1Types["Animal"] {
		t.Errorf("entity 0x1 missing types, got %v", entity1Types)
	}
	if !entity2Types["Mammal"] || !entity2Types["Animal"] {
		t.Errorf("entity 0x2 missing types, got %v", entity2Types)
	}
}

// P2-T03: Inverse Property Materialization
func TestMaterializeInverse(t *testing.T) {
	m := buildTestReasonerAndMaterializer()

	edges := []*pb.DirectedEdge{
		{
			Entity:    0x1, // fido
			Attr:      "hasOwner",
			ValueId:   0x2, // john
			ValueType: pb.Posting_UID,
			Op:        pb.DirectedEdge_SET,
		},
	}

	additional := m.MaterializeInverse(edges)
	if len(additional) != 1 {
		t.Fatalf("expected 1 inverse edge, got %d", len(additional))
	}

	inv := additional[0]
	if inv.Entity != 0x2 {
		t.Errorf("inverse entity should be 0x2 (john), got 0x%x", inv.Entity)
	}
	if inv.Attr != "isOwnerOf" {
		t.Errorf("inverse attr should be isOwnerOf, got %s", inv.Attr)
	}
	if inv.ValueId != 0x1 {
		t.Errorf("inverse valueId should be 0x1 (fido), got 0x%x", inv.ValueId)
	}
}

// Nil materializer should be safe
func TestMaterializeNil(t *testing.T) {
	var m *TypeMaterializer
	additional := m.MaterializeTypes(nil, "dgraph.type")
	if additional != nil {
		t.Error("nil materializer should return nil")
	}
}

// Global materializer
func TestGlobalMaterializer(t *testing.T) {
	m := buildTestReasonerAndMaterializer()
	SetGlobal(m)
	defer SetGlobal(nil)

	got := GetGlobal()
	if got != m {
		t.Error("GetGlobal should return the set materializer")
	}
}
