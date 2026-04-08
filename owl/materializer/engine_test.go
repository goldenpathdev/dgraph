package materializer

import (
	"strings"
	"testing"

	"github.com/dgraph-io/dgraph/v25/owl"
	pb "github.com/dgraph-io/dgraph/v25/protos/pb"
)

func buildTestEngineWithChains() *Engine {
	ont := owl.NewOntology()
	ont.Classes["Person"] = &owl.Class{IRI: "Person"}
	ont.ObjectProperties["hasParent"] = &owl.ObjectProperty{IRI: "hasParent"}
	ont.ObjectProperties["hasGrandparent"] = &owl.ObjectProperty{IRI: "hasGrandparent"}
	ont.PropertyChains = []owl.PropertyChain{
		{Property: "hasGrandparent", Chain: []owl.PropertyIRI{"hasParent", "hasParent"}},
	}
	r := owl.NewReasoner(ont)
	r.Build()
	return NewEngine(ont, r)
}

func buildTestEngine() *Engine {
	ont := owl.NewOntology()
	ont.Classes["Animal"] = &owl.Class{IRI: "Animal"}
	ont.Classes["Mammal"] = &owl.Class{IRI: "Mammal", SuperClasses: []owl.ClassIRI{"Animal"}}
	ont.Classes["Dog"] = &owl.Class{IRI: "Dog", SuperClasses: []owl.ClassIRI{"Mammal"},
		DisjointWith: []owl.ClassIRI{"Cat"}}
	ont.Classes["Cat"] = &owl.Class{IRI: "Cat", SuperClasses: []owl.ClassIRI{"Mammal"},
		DisjointWith: []owl.ClassIRI{"Dog"}}
	ont.Classes["GoldenRetriever"] = &owl.Class{IRI: "GoldenRetriever", SuperClasses: []owl.ClassIRI{"Dog"}}
	ont.Classes["Person"] = &owl.Class{IRI: "Person"}

	ont.ObjectProperties["hasOwner"] = &owl.ObjectProperty{
		IRI:       "hasOwner",
		Domain:    []owl.ClassIRI{"Animal"},
		Range:     []owl.ClassIRI{"Person"},
		InverseOf: "isOwnerOf",
		Characteristics: owl.PropertyCharacteristics{Functional: true},
	}
	ont.ObjectProperties["isOwnerOf"] = &owl.ObjectProperty{
		IRI:       "isOwnerOf",
		Domain:    []owl.ClassIRI{"Person"},
		Range:     []owl.ClassIRI{"Animal"},
		InverseOf: "hasOwner",
	}
	ont.ObjectProperties["friendOf"] = &owl.ObjectProperty{
		IRI: "friendOf",
		Characteristics: owl.PropertyCharacteristics{Symmetric: true},
	}

	r := owl.NewReasoner(ont)
	r.Build()
	return NewEngine(ont, r)
}

// P4-T01: Domain Inference
func TestEngineDomainInference(t *testing.T) {
	eng := buildTestEngine()
	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "hasOwner", ValueId: 0x2, ValueType: pb.Posting_UID, Op: pb.DirectedEdge_SET},
	}

	additional, err := eng.Materialize(edges, "dgraph.type")
	if err != nil {
		t.Fatal(err)
	}

	// Should infer entity 0x1 is Animal (domain of hasOwner) plus Mammal (ancestor)... wait no,
	// Animal has no ancestors. Also Person for 0x2 (range).
	typeMap := collectEntityTypes(additional)

	if !typeMap[0x1]["Animal"] {
		t.Errorf("entity 0x1 should be inferred as Animal (domain of hasOwner), got %v", typeMap[0x1])
	}
}

// P4-T02: Range Inference
func TestEngineRangeInference(t *testing.T) {
	eng := buildTestEngine()
	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "hasOwner", ValueId: 0x2, ValueType: pb.Posting_UID, Op: pb.DirectedEdge_SET},
	}

	additional, err := eng.Materialize(edges, "dgraph.type")
	if err != nil {
		t.Fatal(err)
	}

	typeMap := collectEntityTypes(additional)
	if !typeMap[0x2]["Person"] {
		t.Errorf("entity 0x2 should be inferred as Person (range of hasOwner), got %v", typeMap[0x2])
	}
}

// P4-T03: Symmetric Property
func TestEngineSymmetric(t *testing.T) {
	eng := buildTestEngine()
	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "friendOf", ValueId: 0x2, ValueType: pb.Posting_UID, Op: pb.DirectedEdge_SET},
	}

	additional, err := eng.Materialize(edges, "dgraph.type")
	if err != nil {
		t.Fatal(err)
	}

	// Should have a reverse friendOf edge
	found := false
	for _, e := range additional {
		if e.Entity == 0x2 && e.Attr == "friendOf" && e.ValueId == 0x1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("symmetric property should generate reverse edge: 0x2 friendOf 0x1")
	}
}

// P4-T05: Inverse materialization via engine
func TestEngineInverse(t *testing.T) {
	eng := buildTestEngine()
	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "hasOwner", ValueId: 0x2, ValueType: pb.Posting_UID, Op: pb.DirectedEdge_SET},
	}

	additional, err := eng.Materialize(edges, "dgraph.type")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range additional {
		if e.Entity == 0x2 && e.Attr == "isOwnerOf" && e.ValueId == 0x1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("inverse property should generate: 0x2 isOwnerOf 0x1")
	}
}

// P4-T06: Disjointness Violation
func TestEngineDisjointness(t *testing.T) {
	eng := buildTestEngine()
	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "dgraph.type", Value: []byte("Dog"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
		{Entity: 0x1, Attr: "dgraph.type", Value: []byte("Cat"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
	}

	_, err := eng.Materialize(edges, "dgraph.type")
	if err == nil {
		t.Fatal("expected disjointness error for Dog+Cat")
	}
	if !strings.Contains(err.Error(), "disjoint") {
		t.Errorf("error should mention disjoint, got: %s", err.Error())
	}
}

// P4-T07: Circuit Breaker
func TestEngineCircuitBreaker(t *testing.T) {
	eng := buildTestEngine()

	// Set a very low limit
	oldLimit := MaxInferredEdgesPerMutation
	MaxInferredEdgesPerMutation = 2
	defer func() { MaxInferredEdgesPerMutation = oldLimit }()

	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "dgraph.type", Value: []byte("GoldenRetriever"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
	}

	_, err := eng.Materialize(edges, "dgraph.type")
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
	if !strings.Contains(err.Error(), "circuit breaker") {
		t.Errorf("error should mention circuit breaker, got: %s", err.Error())
	}
}

// Full materialization: type hierarchy + domain + range + inverse + symmetric
func TestEngineFullMaterialization(t *testing.T) {
	eng := buildTestEngine()
	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "dgraph.type", Value: []byte("GoldenRetriever"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_SET},
		{Entity: 0x1, Attr: "hasOwner", ValueId: 0x2, ValueType: pb.Posting_UID, Op: pb.DirectedEdge_SET},
	}

	additional, err := eng.Materialize(edges, "dgraph.type")
	if err != nil {
		t.Fatal(err)
	}

	typeMap := collectEntityTypes(additional)

	// Entity 0x1: GoldenRetriever → Dog, Mammal, Animal (type hierarchy) + Animal (domain)
	if !typeMap[0x1]["Dog"] || !typeMap[0x1]["Mammal"] || !typeMap[0x1]["Animal"] {
		t.Errorf("entity 0x1 missing type hierarchy: %v", typeMap[0x1])
	}

	// Entity 0x2: Person (range of hasOwner)
	if !typeMap[0x2]["Person"] {
		t.Errorf("entity 0x2 should have Person (range): %v", typeMap[0x2])
	}

	// Check inverse edge: 0x2 isOwnerOf 0x1
	found := false
	for _, e := range additional {
		if e.Entity == 0x2 && e.Attr == "isOwnerOf" && e.ValueId == 0x1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing inverse edge: 0x2 isOwnerOf 0x1")
	}
}

// P4-T10: Delete Cascades
func TestEngineDeleteCascade(t *testing.T) {
	eng := buildTestEngine()
	edges := []*pb.DirectedEdge{
		{Entity: 0x1, Attr: "dgraph.type", Value: []byte("GoldenRetriever"), ValueType: pb.Posting_STRING, Op: pb.DirectedEdge_DEL},
	}

	additional, err := eng.Materialize(edges, "dgraph.type")
	if err != nil {
		t.Fatal(err)
	}

	// Should generate DEL for Dog, Mammal, Animal
	delTypes := make(map[string]bool)
	for _, e := range additional {
		if e.Op == pb.DirectedEdge_DEL && e.Attr == "dgraph.type" {
			delTypes[string(e.Value)] = true
		}
	}
	for _, expected := range []string{"Dog", "Mammal", "Animal"} {
		if !delTypes[expected] {
			t.Errorf("missing cascade delete for type: %s", expected)
		}
	}
}

// P4-T04: Property Chain
func TestEnginePropertyChain(t *testing.T) {
	eng := buildTestEngineWithChains()
	edges := []*pb.DirectedEdge{
		{Entity: 0x3, Attr: "hasParent", ValueId: 0x2, ValueType: pb.Posting_UID, Op: pb.DirectedEdge_SET}, // c→b
		{Entity: 0x2, Attr: "hasParent", ValueId: 0x1, ValueType: pb.Posting_UID, Op: pb.DirectedEdge_SET}, // b→a
	}

	additional, err := eng.Materialize(edges, "dgraph.type")
	if err != nil {
		t.Fatal(err)
	}

	// Should infer: c hasGrandparent a (0x3 → 0x1)
	found := false
	for _, e := range additional {
		if e.Entity == 0x3 && e.Attr == "hasGrandparent" && e.ValueId == 0x1 {
			found = true
			if len(e.Facets) == 0 {
				t.Error("chain edge should have owl.inferred facet")
			}
			break
		}
	}
	if !found {
		t.Error("expected inferred edge: 0x3 hasGrandparent 0x1")
	}
}

func collectEntityTypes(edges []*pb.DirectedEdge) map[uint64]map[string]bool {
	m := make(map[uint64]map[string]bool)
	for _, e := range edges {
		if e.Attr == "dgraph.type" && e.Op == pb.DirectedEdge_SET {
			if m[e.Entity] == nil {
				m[e.Entity] = make(map[string]bool)
			}
			m[e.Entity][string(e.Value)] = true
		}
	}
	return m
}
