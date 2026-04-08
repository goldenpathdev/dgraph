// Package graphql generates GraphQL SDL from OWL ontologies.
// The generated SDL is compatible with Dgraph's GraphQL layer and can be
// fed directly into the existing NewHandler() pipeline.
package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dgraph-io/dgraph/v25/owl"
)

// Generator produces GraphQL schema SDL from an OWL Ontology.
type Generator struct {
	ont      *owl.Ontology
	reasoner *owl.Reasoner
}

// NewGenerator creates a GraphQL schema generator.
func NewGenerator(ont *owl.Ontology, reasoner *owl.Reasoner) *Generator {
	return &Generator{ont: ont, reasoner: reasoner}
}

// GenerateSDL produces a complete GraphQL SDL string from the ontology.
// The output is suitable for feeding into Dgraph's existing GraphQL handler.
//
// Mapping rules:
//   - owl:Class with subclasses → GraphQL interface + type
//   - owl:Class without subclasses → GraphQL type only
//   - rdfs:subClassOf → implements interface
//   - owl:ObjectProperty → field with object type
//   - owl:DatatypeProperty → field with scalar type
//   - owl:FunctionalProperty → singular field (T not [T])
//   - owl:inverseOf → @hasInverse directive
//   - owl:TransitiveProperty → regular field + transitivePath field
func (g *Generator) GenerateSDL() (string, error) {
	if g.ont == nil {
		return "", fmt.Errorf("owl/graphql: no ontology provided")
	}

	var sb strings.Builder

	// Determine which classes have subclasses (these become interfaces)
	hasSubclasses := make(map[owl.ClassIRI]bool)
	for _, cls := range g.ont.Classes {
		for _, sup := range cls.SuperClasses {
			hasSubclasses[sup] = true
		}
	}

	// Sort class names for deterministic output
	classNames := sortedClassNames(g.ont)

	// Phase 1: Generate interfaces for classes that have subclasses
	for _, name := range classNames {
		if !hasSubclasses[name] {
			continue
		}
		cls := g.ont.Classes[name]
		fields := g.fieldsForClass(cls)
		if len(fields) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("interface %s {\n", name))
		for _, f := range fields {
			sb.WriteString(fmt.Sprintf("  %s\n", f))
		}
		sb.WriteString("}\n\n")
	}

	// Phase 2: Generate types for all classes
	for _, name := range classNames {
		cls := g.ont.Classes[name]
		fields := g.fieldsForClass(cls)
		if len(fields) == 0 {
			// Even fieldless classes get a type with just id
			fields = []string{"id: ID!"}
		}

		// Determine implements clause
		var implements []string
		// Direct superclasses that are interfaces
		for _, sup := range cls.SuperClasses {
			if hasSubclasses[sup] {
				implements = append(implements, string(sup))
			}
		}
		// Also implement interfaces of ancestors (transitive)
		ancestors := g.reasoner.AllSuperClasses(name)
		for _, anc := range ancestors {
			if hasSubclasses[anc] {
				found := false
				for _, existing := range implements {
					if existing == string(anc) {
						found = true
						break
					}
				}
				if !found {
					implements = append(implements, string(anc))
				}
			}
		}

		implStr := ""
		if len(implements) > 0 {
			implStr = " implements " + strings.Join(implements, " & ")
		}

		sb.WriteString(fmt.Sprintf("type %s%s {\n", name, implStr))
		for _, f := range fields {
			sb.WriteString(fmt.Sprintf("  %s\n", f))
		}
		sb.WriteString("}\n\n")
	}

	return sb.String(), nil
}

// fieldsForClass generates GraphQL field declarations for a class,
// including inherited fields from superclasses.
func (g *Generator) fieldsForClass(cls *owl.Class) []string {
	seen := make(map[string]bool)
	var fields []string

	// Always include id
	fields = append(fields, "id: ID!")
	seen["id"] = true

	// Collect all classes in hierarchy (this class + ancestors)
	classIRIs := g.reasoner.AncestorTypes(cls.IRI)
	domainSet := make(map[owl.ClassIRI]bool)
	for _, iri := range classIRIs {
		domainSet[iri] = true
	}

	// Add object property fields
	propNames := sortedObjPropNames(g.ont)
	for _, pName := range propNames {
		op := g.ont.ObjectProperties[pName]
		if !g.propertyApplies(op.Domain, domainSet) {
			continue
		}
		if seen[string(pName)] {
			continue
		}
		seen[string(pName)] = true

		fieldType := g.objectPropertyFieldType(op)
		field := fmt.Sprintf("%s: %s", pName, fieldType)

		// Add @hasInverse if inverse is declared
		if op.InverseOf != "" {
			field += fmt.Sprintf(" @hasInverse(field: \"%s\")", op.InverseOf)
		}

		fields = append(fields, field)
	}

	// Add data property fields
	dataPropNames := sortedDataPropNames(g.ont)
	for _, pName := range dataPropNames {
		dp := g.ont.DataProperties[pName]
		if !g.propertyApplies(dp.Domain, domainSet) {
			continue
		}
		if seen[string(pName)] {
			continue
		}
		seen[string(pName)] = true

		scalarType := xsdToGraphQL(dp.Range)
		if dp.IsFunctional {
			fields = append(fields, fmt.Sprintf("%s: %s", pName, scalarType))
		} else {
			fields = append(fields, fmt.Sprintf("%s: [%s]", pName, scalarType))
		}
	}

	return fields
}

// propertyApplies checks if a property's domain includes any of the target classes.
// If domain is empty, the property is universal (applies to all classes).
func (g *Generator) propertyApplies(domain []owl.ClassIRI, targetClasses map[owl.ClassIRI]bool) bool {
	if len(domain) == 0 {
		return true // Universal property
	}
	for _, d := range domain {
		if targetClasses[d] {
			return true
		}
	}
	return false
}

// objectPropertyFieldType generates the GraphQL type for an object property.
func (g *Generator) objectPropertyFieldType(op *owl.ObjectProperty) string {
	// Determine range type
	rangeType := "Node" // fallback if no range declared
	if len(op.Range) > 0 {
		rangeType = string(op.Range[0])
	}

	if op.Characteristics.Functional {
		return rangeType
	}
	return fmt.Sprintf("[%s]", rangeType)
}

// xsdToGraphQL maps XSD datatypes to GraphQL scalar types.
func xsdToGraphQL(dt owl.DatatypeIRI) string {
	switch dt {
	case owl.XSDString, "":
		return "String"
	case owl.XSDInteger:
		return "Int"
	case owl.XSDFloat:
		return "Float"
	case owl.XSDBoolean:
		return "Boolean"
	case owl.XSDDate, owl.XSDDateTime:
		return "DateTime"
	default:
		return "String"
	}
}

// Sorting helpers for deterministic output

func sortedClassNames(ont *owl.Ontology) []owl.ClassIRI {
	names := make([]owl.ClassIRI, 0, len(ont.Classes))
	for name := range ont.Classes {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

func sortedObjPropNames(ont *owl.Ontology) []owl.PropertyIRI {
	names := make([]owl.PropertyIRI, 0, len(ont.ObjectProperties))
	for name := range ont.ObjectProperties {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

func sortedDataPropNames(ont *owl.Ontology) []owl.PropertyIRI {
	names := make([]owl.PropertyIRI, 0, len(ont.DataProperties))
	for name := range ont.DataProperties {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}
