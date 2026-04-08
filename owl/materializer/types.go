// Package materializer handles write-time OWL inference and triple materialization.
package materializer

import (
	"sync"

	"github.com/dgraph-io/dgraph/v25/owl"
	pb "github.com/dgraph-io/dgraph/v25/protos/pb"
)

// TypeMaterializer adds inferred type edges during mutations.
// It is safe for concurrent use.
type TypeMaterializer struct {
	mu       sync.RWMutex
	reasoner *owl.Reasoner
}

var (
	// Global materializer instance, set when ontology is loaded.
	globalMaterializer *TypeMaterializer
	globalEngine       *Engine
	globalMu           sync.RWMutex
)

// NewTypeMaterializer creates a materializer backed by the given reasoner.
func NewTypeMaterializer(reasoner *owl.Reasoner) *TypeMaterializer {
	return &TypeMaterializer{reasoner: reasoner}
}

// SetGlobal sets the global materializer instance.
// Called when an ontology is loaded.
func SetGlobal(m *TypeMaterializer) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalMaterializer = m
}

// GetGlobal returns the current global materializer, or nil if none is set.
func GetGlobal() *TypeMaterializer {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalMaterializer
}

// SetGlobalEngine sets the global reasoning engine.
func SetGlobalEngine(e *Engine) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalEngine = e
}

// GetGlobalEngine returns the current global engine, or nil if none is set.
func GetGlobalEngine() *Engine {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalEngine
}

// MaterializeTypes takes a set of directed edges and returns additional edges
// representing inferred type hierarchy memberships.
//
// For each edge that sets dgraph.type = "SomeType", the materializer looks up
// all ancestor types and appends edges for them.
//
// The dgraphTypeAttr parameter is the namespace-qualified attribute name for
// dgraph.type (e.g., "dgraph.type" or with namespace prefix).
func (m *TypeMaterializer) MaterializeTypes(edges []*pb.DirectedEdge, dgraphTypeAttr string) []*pb.DirectedEdge {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.reasoner == nil {
		return nil
	}

	var additional []*pb.DirectedEdge

	for _, edge := range edges {
		// Only process SET operations on dgraph.type
		if edge.Attr != dgraphTypeAttr {
			continue
		}
		if edge.Op != pb.DirectedEdge_SET {
			continue
		}

		// Extract the type name from the value
		typeName := string(edge.Value)
		if typeName == "" {
			continue
		}

		// Look up ancestor types
		ancestors := m.reasoner.AllSuperClasses(owl.ClassIRI(typeName))
		if len(ancestors) == 0 {
			continue
		}

		// Create additional type edges for each ancestor
		for _, ancestor := range ancestors {
			additional = append(additional, &pb.DirectedEdge{
				Entity:    edge.Entity,
				Attr:      dgraphTypeAttr,
				Value:     []byte(string(ancestor)),
				ValueType: edge.ValueType,
				Op:        pb.DirectedEdge_SET,
				Namespace: edge.Namespace,
			})
		}
	}

	return additional
}

// MaterializeInverse takes a set of directed edges and returns additional edges
// for inverse property materialization.
func (m *TypeMaterializer) MaterializeInverse(edges []*pb.DirectedEdge) []*pb.DirectedEdge {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.reasoner == nil {
		return nil
	}

	var additional []*pb.DirectedEdge

	for _, edge := range edges {
		if edge.Op != pb.DirectedEdge_SET {
			continue
		}
		// Check if this predicate has an inverse
		inv, ok := m.reasoner.InverseProperty(owl.PropertyIRI(edge.Attr))
		if !ok || edge.ValueId == 0 {
			continue
		}

		// Create reverse edge
		additional = append(additional, &pb.DirectedEdge{
			Entity:    edge.ValueId,
			Attr:      string(inv),
			ValueId:   edge.Entity,
			ValueType: pb.Posting_UID,
			Op:        pb.DirectedEdge_SET,
			Namespace: edge.Namespace,
		})
	}

	return additional
}
