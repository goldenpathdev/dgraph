package materializer

import (
	"fmt"

	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/dgraph-io/dgraph/v25/owl"
	pb "github.com/dgraph-io/dgraph/v25/protos/pb"
)

// inferredFacet creates the owl.inferred=true facet to mark materialized triples.
func inferredFacet() []*api.Facet {
	return []*api.Facet{
		{
			Key:     "owl.inferred",
			Value:   []byte{0x01}, // true
			ValType: api.Facet_BOOL,
		},
	}
}

// MaxInferredEdgesPerMutation is the circuit breaker limit.
// If a single mutation would generate more than this many inferred edges,
// the materialization is aborted and an error is returned.
var MaxInferredEdgesPerMutation = 10000

// Engine is the full OWL2 RL write-time reasoning engine.
// It applies all inference rules to a set of mutation edges and returns
// the additional inferred edges that should be added to the mutation.
type Engine struct {
	reasoner *owl.Reasoner
	ont      *owl.Ontology
}

// NewEngine creates a reasoning engine backed by the given ontology and reasoner.
func NewEngine(ont *owl.Ontology, reasoner *owl.Reasoner) *Engine {
	return &Engine{ont: ont, reasoner: reasoner}
}

// Materialize applies all OWL2 RL inference rules to the given edges and returns
// additional edges to be appended to the mutation.
//
// Rules applied (in order):
// 1. Type hierarchy materialization (subClassOf)
// 2. Domain inference (property domain → subject type)
// 3. Range inference (property range → object type)
// 4. Inverse property materialization (inverseOf)
// 5. Symmetric property materialization
// 6. Disjointness validation (returns error if violated)
func (e *Engine) Materialize(edges []*pb.DirectedEdge, dgraphTypeAttr string) ([]*pb.DirectedEdge, error) {
	var all []*pb.DirectedEdge

	// Rule 0: Delete cascades — when a type is removed, remove inferred ancestors
	deleteEdges := e.cascadeDeletes(edges, dgraphTypeAttr)
	all = append(all, deleteEdges...)

	// Rule 1: Type hierarchy
	typeEdges := e.materializeTypeHierarchy(edges, dgraphTypeAttr)
	all = append(all, typeEdges...)

	// Rule 2: Domain inference
	domainEdges := e.materializeDomain(edges, dgraphTypeAttr)
	all = append(all, domainEdges...)

	// Rule 3: Range inference
	rangeEdges := e.materializeRange(edges, dgraphTypeAttr)
	all = append(all, rangeEdges...)

	// Rule 4: Inverse properties
	inverseEdges := e.materializeInverse(edges)
	all = append(all, inverseEdges...)

	// Rule 5: Symmetric properties
	symmetricEdges := e.materializeSymmetric(edges)
	all = append(all, symmetricEdges...)

	// Rule 6: Property chains
	chainEdges := e.materializePropertyChains(edges)
	all = append(all, chainEdges...)

	// Rule 7: Disjointness validation
	if err := e.validateDisjointness(edges, all, dgraphTypeAttr); err != nil {
		return nil, err
	}

	// Circuit breaker
	if len(all) > MaxInferredEdgesPerMutation {
		return nil, fmt.Errorf("owl/materializer: circuit breaker triggered: %d inferred edges exceeds limit of %d",
			len(all), MaxInferredEdgesPerMutation)
	}

	return all, nil
}

// materializeTypeHierarchy adds ancestor type edges for each dgraph.type SET.
func (e *Engine) materializeTypeHierarchy(edges []*pb.DirectedEdge, dgraphTypeAttr string) []*pb.DirectedEdge {
	var additional []*pb.DirectedEdge
	for _, edge := range edges {
		if edge.Attr != dgraphTypeAttr || edge.Op != pb.DirectedEdge_SET {
			continue
		}
		typeName := string(edge.Value)
		if typeName == "" {
			continue
		}
		ancestors := e.reasoner.AllSuperClasses(owl.ClassIRI(typeName))
		for _, ancestor := range ancestors {
			additional = append(additional, &pb.DirectedEdge{
				Entity:    edge.Entity,
				Attr:      dgraphTypeAttr,
				Value:     []byte(string(ancestor)),
				ValueType: edge.ValueType,
				Op:        pb.DirectedEdge_SET,
				Namespace: edge.Namespace,
				Facets:    inferredFacet(),
			})
		}
	}
	return additional
}

// materializeDomain adds type edges based on property domain declarations.
// If hasOwner has domain=Animal, then writing <x> <hasOwner> <y> infers <x> is Animal.
func (e *Engine) materializeDomain(edges []*pb.DirectedEdge, dgraphTypeAttr string) []*pb.DirectedEdge {
	var additional []*pb.DirectedEdge
	for _, edge := range edges {
		if edge.Op != pb.DirectedEdge_SET {
			continue
		}
		domainTypes := e.reasoner.InferredTypes(owl.PropertyIRI(edge.Attr), true)
		for _, dt := range domainTypes {
			additional = append(additional, &pb.DirectedEdge{
				Entity:    edge.Entity,
				Attr:      dgraphTypeAttr,
				Value:     []byte(string(dt)),
				ValueType: pb.Posting_STRING,
				Op:        pb.DirectedEdge_SET,
				Namespace: edge.Namespace,
				Facets:    inferredFacet(),
			})
			// Also add ancestors of the domain type
			ancestors := e.reasoner.AllSuperClasses(dt)
			for _, anc := range ancestors {
				additional = append(additional, &pb.DirectedEdge{
					Entity:    edge.Entity,
					Attr:      dgraphTypeAttr,
					Value:     []byte(string(anc)),
					ValueType: pb.Posting_STRING,
					Op:        pb.DirectedEdge_SET,
					Namespace: edge.Namespace,
					Facets:    inferredFacet(),
				})
			}
		}
	}
	return additional
}

// materializeRange adds type edges based on property range declarations.
// If hasOwner has range=Person, then writing <x> <hasOwner> <y> infers <y> is Person.
func (e *Engine) materializeRange(edges []*pb.DirectedEdge, dgraphTypeAttr string) []*pb.DirectedEdge {
	var additional []*pb.DirectedEdge
	for _, edge := range edges {
		if edge.Op != pb.DirectedEdge_SET || edge.ValueId == 0 {
			continue
		}
		rangeTypes := e.reasoner.InferredTypes(owl.PropertyIRI(edge.Attr), false)
		for _, rt := range rangeTypes {
			additional = append(additional, &pb.DirectedEdge{
				Entity:    edge.ValueId,
				Attr:      dgraphTypeAttr,
				Value:     []byte(string(rt)),
				ValueType: pb.Posting_STRING,
				Op:        pb.DirectedEdge_SET,
				Namespace: edge.Namespace,
				Facets:    inferredFacet(),
			})
			ancestors := e.reasoner.AllSuperClasses(rt)
			for _, anc := range ancestors {
				additional = append(additional, &pb.DirectedEdge{
					Entity:    edge.ValueId,
					Attr:      dgraphTypeAttr,
					Value:     []byte(string(anc)),
					ValueType: pb.Posting_STRING,
					Op:        pb.DirectedEdge_SET,
					Namespace: edge.Namespace,
					Facets:    inferredFacet(),
				})
			}
		}
	}
	return additional
}

// materializeInverse creates reverse edges for properties with owl:inverseOf.
func (e *Engine) materializeInverse(edges []*pb.DirectedEdge) []*pb.DirectedEdge {
	var additional []*pb.DirectedEdge
	for _, edge := range edges {
		if edge.Op != pb.DirectedEdge_SET || edge.ValueId == 0 {
			continue
		}
		inv, ok := e.reasoner.InverseProperty(owl.PropertyIRI(edge.Attr))
		if !ok {
			continue
		}
		additional = append(additional, &pb.DirectedEdge{
			Entity:    edge.ValueId,
			Attr:      string(inv),
			ValueId:   edge.Entity,
			ValueType: pb.Posting_UID,
			Op:        pb.DirectedEdge_SET,
			Namespace: edge.Namespace,
			Facets:    inferredFacet(),
		})
	}
	return additional
}

// materializeSymmetric creates reverse edges for symmetric properties.
func (e *Engine) materializeSymmetric(edges []*pb.DirectedEdge) []*pb.DirectedEdge {
	var additional []*pb.DirectedEdge
	for _, edge := range edges {
		if edge.Op != pb.DirectedEdge_SET || edge.ValueId == 0 {
			continue
		}
		if !e.reasoner.IsSymmetric(owl.PropertyIRI(edge.Attr)) {
			continue
		}
		additional = append(additional, &pb.DirectedEdge{
			Entity:    edge.ValueId,
			Attr:      edge.Attr,
			ValueId:   edge.Entity,
			ValueType: pb.Posting_UID,
			Op:        pb.DirectedEdge_SET,
			Facets:    inferredFacet(),
			Namespace: edge.Namespace,
		})
	}
	return additional
}

// validateDisjointness checks that no entity has types that are declared disjoint.
func (e *Engine) validateDisjointness(original, inferred []*pb.DirectedEdge, dgraphTypeAttr string) error {
	// Collect all types per entity (original + inferred)
	entityTypes := make(map[uint64]map[string]bool)
	collectTypes := func(edges []*pb.DirectedEdge) {
		for _, edge := range edges {
			if edge.Attr != dgraphTypeAttr || edge.Op != pb.DirectedEdge_SET {
				continue
			}
			if entityTypes[edge.Entity] == nil {
				entityTypes[edge.Entity] = make(map[string]bool)
			}
			entityTypes[edge.Entity][string(edge.Value)] = true
		}
	}
	collectTypes(original)
	collectTypes(inferred)

	// Check for disjointness violations
	for entity, types := range entityTypes {
		for typeName := range types {
			cls, ok := e.ont.Classes[owl.ClassIRI(typeName)]
			if !ok {
				continue
			}
			for _, disj := range cls.DisjointWith {
				if types[string(disj)] {
					return fmt.Errorf("owl/materializer: disjointness violation on entity 0x%x: "+
						"type %q is disjoint with type %q", entity, typeName, disj)
				}
			}
		}
	}
	return nil
}

// cascadeDeletes generates DELETE edges for ancestor types when a type is removed.
func (e *Engine) cascadeDeletes(edges []*pb.DirectedEdge, dgraphTypeAttr string) []*pb.DirectedEdge {
	var additional []*pb.DirectedEdge
	for _, edge := range edges {
		if edge.Attr != dgraphTypeAttr || edge.Op != pb.DirectedEdge_DEL {
			continue
		}
		typeName := string(edge.Value)
		if typeName == "" {
			continue
		}
		ancestors := e.reasoner.AllSuperClasses(owl.ClassIRI(typeName))
		for _, ancestor := range ancestors {
			additional = append(additional, &pb.DirectedEdge{
				Entity:    edge.Entity,
				Attr:      dgraphTypeAttr,
				Value:     []byte(string(ancestor)),
				ValueType: edge.ValueType,
				Op:        pb.DirectedEdge_DEL,
				Namespace: edge.Namespace,
			})
		}
	}
	return additional
}

// materializePropertyChains handles owl:propertyChainAxiom.
// If hasGrandparent = hasParent o hasParent, and the mutation contains
// <c> hasParent <b> and <b> hasParent <a>, then infer <c> hasGrandparent <a>.
// Only handles chains present within the same mutation batch.
func (e *Engine) materializePropertyChains(edges []*pb.DirectedEdge) []*pb.DirectedEdge {
	if len(e.ont.PropertyChains) == 0 {
		return nil
	}

	// Build index: predicate → list of (subject, object) pairs
	type link struct {
		subject uint64
		object  uint64
		ns      uint64
	}
	predIndex := make(map[string][]link)
	for _, edge := range edges {
		if edge.Op != pb.DirectedEdge_SET || edge.ValueId == 0 {
			continue
		}
		predIndex[edge.Attr] = append(predIndex[edge.Attr], link{edge.Entity, edge.ValueId, edge.Namespace})
	}

	var additional []*pb.DirectedEdge

	for _, chain := range e.ont.PropertyChains {
		if len(chain.Chain) != 2 {
			continue // Only support length-2 chains for now
		}
		prop1 := string(chain.Chain[0])
		prop2 := string(chain.Chain[1])
		resultProp := string(chain.Property)

		links1 := predIndex[prop1]
		links2 := predIndex[prop2]

		// For each link1 (a→b via prop1), find link2 (b→c via prop2)
		// Result: a→c via resultProp
		for _, l1 := range links1 {
			for _, l2 := range links2 {
				if l1.object == l2.subject {
					additional = append(additional, &pb.DirectedEdge{
						Entity:    l1.subject,
						Attr:      resultProp,
						ValueId:   l2.object,
						ValueType: pb.Posting_UID,
						Op:        pb.DirectedEdge_SET,
						Namespace: l1.ns,
						Facets:    inferredFacet(),
					})
				}
			}
		}
	}

	return additional
}
