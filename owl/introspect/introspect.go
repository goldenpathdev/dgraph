// Package introspect provides ontology introspection capabilities.
// It allows querying the loaded ontology for class hierarchies,
// property definitions, and other structural information.
package introspect

import (
	"github.com/dgraph-io/dgraph/v25/owl"
)

// ClassInfo represents introspection data about an OWL class.
type ClassInfo struct {
	IRI            string      `json:"iri"`
	Label          string      `json:"label,omitempty"`
	SuperClasses   []string    `json:"superClasses,omitempty"`
	SubClasses     []string    `json:"subClasses,omitempty"`
	DisjointWith   []string    `json:"disjointWith,omitempty"`
	Properties     []PropInfo  `json:"properties,omitempty"`
}

// PropInfo represents introspection data about an OWL property.
type PropInfo struct {
	IRI             string   `json:"iri"`
	Label           string   `json:"label,omitempty"`
	Type            string   `json:"type"` // "ObjectProperty" or "DataProperty"
	Domain          []string `json:"domain,omitempty"`
	Range           []string `json:"range,omitempty"`
	IsFunctional    bool     `json:"isFunctional,omitempty"`
	IsTransitive    bool     `json:"isTransitive,omitempty"`
	IsSymmetric     bool     `json:"isSymmetric,omitempty"`
	InverseOf       string   `json:"inverseOf,omitempty"`
}

// Inspector provides ontology introspection operations.
type Inspector struct {
	ont      *owl.Ontology
	reasoner *owl.Reasoner
}

// NewInspector creates an inspector for the given ontology.
func NewInspector(ont *owl.Ontology, reasoner *owl.Reasoner) *Inspector {
	return &Inspector{ont: ont, reasoner: reasoner}
}

// ListClasses returns info about all classes in the ontology.
func (i *Inspector) ListClasses() []ClassInfo {
	var classes []ClassInfo
	for iri, cls := range i.ont.Classes {
		info := ClassInfo{
			IRI:   string(iri),
			Label: cls.Label,
		}
		for _, s := range cls.SuperClasses {
			info.SuperClasses = append(info.SuperClasses, string(s))
		}
		for _, d := range cls.DisjointWith {
			info.DisjointWith = append(info.DisjointWith, string(d))
		}
		for _, sub := range i.reasoner.DirectSubClasses(iri) {
			info.SubClasses = append(info.SubClasses, string(sub))
		}
		info.Properties = i.PropertiesOf(string(iri))
		classes = append(classes, info)
	}
	return classes
}

// GetClass returns info about a specific class.
func (i *Inspector) GetClass(name string) *ClassInfo {
	iri := owl.ClassIRI(name)
	cls, ok := i.ont.Classes[iri]
	if !ok {
		return nil
	}
	info := &ClassInfo{
		IRI:   string(iri),
		Label: cls.Label,
	}
	for _, s := range cls.SuperClasses {
		info.SuperClasses = append(info.SuperClasses, string(s))
	}
	for _, d := range cls.DisjointWith {
		info.DisjointWith = append(info.DisjointWith, string(d))
	}
	for _, sub := range i.reasoner.DirectSubClasses(iri) {
		info.SubClasses = append(info.SubClasses, string(sub))
	}
	// Include transitive sub/super classes
	for _, sup := range i.reasoner.AllSuperClasses(iri) {
		found := false
		for _, existing := range info.SuperClasses {
			if existing == string(sup) {
				found = true
				break
			}
		}
		if !found {
			info.SuperClasses = append(info.SuperClasses, string(sup))
		}
	}
	info.Properties = i.PropertiesOf(name)
	return info
}

// PropertiesOf returns all properties applicable to a class (including inherited).
func (i *Inspector) PropertiesOf(className string) []PropInfo {
	cls := owl.ClassIRI(className)
	ancestors := i.reasoner.AncestorTypes(cls)
	domainSet := make(map[owl.ClassIRI]bool)
	for _, a := range ancestors {
		domainSet[a] = true
	}

	var props []PropInfo

	for iri, op := range i.ont.ObjectProperties {
		applies := len(op.Domain) == 0 // universal
		for _, d := range op.Domain {
			if domainSet[d] {
				applies = true
				break
			}
		}
		if !applies {
			continue
		}

		p := PropInfo{
			IRI:          string(iri),
			Label:        op.Label,
			Type:         "ObjectProperty",
			IsFunctional: op.Characteristics.Functional,
			IsTransitive: op.Characteristics.Transitive,
			IsSymmetric:  op.Characteristics.Symmetric,
		}
		if op.InverseOf != "" {
			p.InverseOf = string(op.InverseOf)
		}
		for _, d := range op.Domain {
			p.Domain = append(p.Domain, string(d))
		}
		for _, r := range op.Range {
			p.Range = append(p.Range, string(r))
		}
		props = append(props, p)
	}

	for iri, dp := range i.ont.DataProperties {
		applies := len(dp.Domain) == 0
		for _, d := range dp.Domain {
			if domainSet[d] {
				applies = true
				break
			}
		}
		if !applies {
			continue
		}

		p := PropInfo{
			IRI:          string(iri),
			Label:        dp.Label,
			Type:         "DataProperty",
			IsFunctional: dp.IsFunctional,
		}
		for _, d := range dp.Domain {
			p.Domain = append(p.Domain, string(d))
		}
		if dp.Range != "" {
			p.Range = []string{string(dp.Range)}
		}
		props = append(props, p)
	}

	return props
}

// SubClassesOf returns all subclasses (transitive) of the given class.
func (i *Inspector) SubClassesOf(className string, transitive bool) []string {
	iri := owl.ClassIRI(className)
	var result []string
	if transitive {
		for _, sub := range i.reasoner.AllSubClasses(iri) {
			result = append(result, string(sub))
		}
	} else {
		for _, sub := range i.reasoner.DirectSubClasses(iri) {
			result = append(result, string(sub))
		}
	}
	return result
}

// SuperClassesOf returns all superclasses (transitive) of the given class.
func (i *Inspector) SuperClassesOf(className string, transitive bool) []string {
	iri := owl.ClassIRI(className)
	var result []string
	if transitive {
		for _, sup := range i.reasoner.AllSuperClasses(iri) {
			result = append(result, string(sup))
		}
	} else {
		for _, sup := range i.reasoner.DirectSuperClasses(iri) {
			result = append(result, string(sup))
		}
	}
	return result
}
