// Package compiler translates OWL ontologies to Dgraph schema structures.
package compiler

import (
	"fmt"
	"sort"

	"github.com/dgraph-io/dgraph/v25/owl"
	pb "github.com/dgraph-io/dgraph/v25/protos/pb"
)

// DgraphCompiler compiles an OWL Ontology into Dgraph schema predicates and types.
type DgraphCompiler struct {
	ont      *owl.Ontology
	reasoner *owl.Reasoner
}

// NewDgraphCompiler creates a compiler for the given ontology.
func NewDgraphCompiler(ont *owl.Ontology, reasoner *owl.Reasoner) *DgraphCompiler {
	return &DgraphCompiler{ont: ont, reasoner: reasoner}
}

// CompileResult holds the compiled Dgraph schema structures.
type CompileResult struct {
	Predicates []*pb.SchemaUpdate
	Types      []*pb.TypeUpdate
}

// Compile translates the ontology into Dgraph schema predicates and types.
func (c *DgraphCompiler) Compile() (*CompileResult, error) {
	if c.ont == nil {
		return nil, fmt.Errorf("owl/compiler: no ontology provided")
	}

	result := &CompileResult{}
	predicateSet := make(map[string]*pb.SchemaUpdate)

	// Compile data properties → predicates
	for _, dp := range c.ont.DataProperties {
		pred := &pb.SchemaUpdate{
			Predicate: string(dp.IRI),
			ValueType: xsdToDgraph(dp.Range),
			List:      !dp.IsFunctional,
		}
		predicateSet[pred.Predicate] = pred
	}

	// Compile object properties → predicates
	for _, op := range c.ont.ObjectProperties {
		pred := &pb.SchemaUpdate{
			Predicate: string(op.IRI),
			ValueType: pb.Posting_UID,
			List:      !op.Characteristics.Functional,
		}
		// Add @reverse for inverse-capable properties
		if op.InverseOf != "" {
			pred.Directive = pb.SchemaUpdate_REVERSE
		}
		predicateSet[pred.Predicate] = pred
	}

	// Collect predicates into result (sorted for determinism)
	for _, pred := range predicateSet {
		result.Predicates = append(result.Predicates, pred)
	}
	sort.Slice(result.Predicates, func(i, j int) bool {
		return result.Predicates[i].Predicate < result.Predicates[j].Predicate
	})

	// Compile classes → types
	// Each type includes its own properties plus all inherited properties from superclasses
	for _, cls := range c.ont.Classes {
		fields := c.collectFields(cls)
		if len(fields) == 0 {
			continue // Skip types with no fields
		}

		tu := &pb.TypeUpdate{
			TypeName: string(cls.IRI),
			Fields:   fields,
		}
		result.Types = append(result.Types, tu)
	}

	sort.Slice(result.Types, func(i, j int) bool {
		return result.Types[i].TypeName < result.Types[j].TypeName
	})

	return result, nil
}

// collectFields gathers all fields for a class, including inherited ones.
func (c *DgraphCompiler) collectFields(cls *owl.Class) []*pb.SchemaUpdate {
	seen := make(map[string]bool)
	var fields []*pb.SchemaUpdate

	addField := func(predName string) {
		if seen[predName] {
			return
		}
		seen[predName] = true
		fields = append(fields, &pb.SchemaUpdate{Predicate: predName})
	}

	// Collect properties where this class (or an ancestor) is in the domain
	classIRIs := c.reasoner.AncestorTypes(cls.IRI)
	domainSet := make(map[owl.ClassIRI]bool)
	for _, iri := range classIRIs {
		domainSet[iri] = true
	}

	for _, op := range c.ont.ObjectProperties {
		for _, d := range op.Domain {
			if domainSet[d] {
				addField(string(op.IRI))
				break
			}
		}
	}

	for _, dp := range c.ont.DataProperties {
		for _, d := range dp.Domain {
			if domainSet[d] {
				addField(string(dp.IRI))
				break
			}
		}
	}

	// Also add properties with no domain (universal properties)
	for _, op := range c.ont.ObjectProperties {
		if len(op.Domain) == 0 {
			addField(string(op.IRI))
		}
	}
	for _, dp := range c.ont.DataProperties {
		if len(dp.Domain) == 0 {
			addField(string(dp.IRI))
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Predicate < fields[j].Predicate
	})

	return fields
}

// xsdToDgraph maps XSD datatypes to Dgraph value types.
func xsdToDgraph(dt owl.DatatypeIRI) pb.Posting_ValType {
	switch dt {
	case owl.XSDString, "":
		return pb.Posting_STRING
	case owl.XSDInteger:
		return pb.Posting_INT
	case owl.XSDFloat:
		return pb.Posting_FLOAT
	case owl.XSDBoolean:
		return pb.Posting_BOOL
	case owl.XSDDate, owl.XSDDateTime:
		return pb.Posting_DATETIME
	default:
		// Unknown datatype defaults to string
		return pb.Posting_STRING
	}
}

// CompileSchemaString produces a Dgraph schema string from the compile result.
// This is useful for the /alter endpoint.
func CompileSchemaString(result *CompileResult) string {
	var s string
	for _, pred := range result.Predicates {
		s += pred.Predicate + ": "
		switch pred.ValueType {
		case pb.Posting_UID:
			s += "uid"
		case pb.Posting_STRING:
			s += "string"
		case pb.Posting_INT:
			s += "int"
		case pb.Posting_FLOAT:
			s += "float"
		case pb.Posting_BOOL:
			s += "bool"
		case pb.Posting_DATETIME:
			s += "datetime"
		default:
			s += "string"
		}

		if pred.Directive == pb.SchemaUpdate_REVERSE {
			s += " @reverse"
		}
		if pred.Directive == pb.SchemaUpdate_INDEX {
			s += " @index(" + joinTokenizers(pred.Tokenizer) + ")"
		}
		if pred.List {
			s += " @count"
		}
		s += " .\n"
	}
	s += "\n"

	for _, typ := range result.Types {
		s += "type " + typ.TypeName + " {\n"
		for _, f := range typ.Fields {
			s += "  " + f.Predicate + "\n"
		}
		s += "}\n\n"
	}

	return s
}

func joinTokenizers(toks []string) string {
	if len(toks) == 0 {
		return ""
	}
	s := toks[0]
	for _, t := range toks[1:] {
		s += ", " + t
	}
	return s
}
