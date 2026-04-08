package owl

import "fmt"

// Reasoner provides OWL2 RL structural reasoning over an Ontology.
type Reasoner struct {
	ont *Ontology

	// Precomputed indexes (populated by Build)
	subClassIndex   map[ClassIRI][]ClassIRI    // class -> direct subclasses
	superClassIndex map[ClassIRI][]ClassIRI    // class -> direct superclasses
	allSubClasses   map[ClassIRI][]ClassIRI    // class -> transitive subclasses
	allSuperClasses map[ClassIRI][]ClassIRI    // class -> transitive superclasses
}

// NewReasoner creates a Reasoner for the given ontology.
// Call Build() to precompute indexes.
func NewReasoner(ont *Ontology) *Reasoner {
	return &Reasoner{ont: ont}
}

// Build precomputes all reasoning indexes. Must be called before query methods.
func (r *Reasoner) Build() error {
	if r.ont == nil {
		return fmt.Errorf("owl: reasoner has no ontology")
	}
	r.buildClassHierarchy()
	return nil
}

func (r *Reasoner) buildClassHierarchy() {
	r.subClassIndex = make(map[ClassIRI][]ClassIRI)
	r.superClassIndex = make(map[ClassIRI][]ClassIRI)
	r.allSubClasses = make(map[ClassIRI][]ClassIRI)
	r.allSuperClasses = make(map[ClassIRI][]ClassIRI)

	// Build direct indexes from declared subClassOf
	for iri, cls := range r.ont.Classes {
		for _, sup := range cls.SuperClasses {
			r.superClassIndex[iri] = append(r.superClassIndex[iri], sup)
			r.subClassIndex[sup] = append(r.subClassIndex[sup], iri)
		}
	}

	// Compute transitive closures
	for iri := range r.ont.Classes {
		r.allSubClasses[iri] = r.collectTransitive(iri, r.subClassIndex)
		r.allSuperClasses[iri] = r.collectTransitive(iri, r.superClassIndex)
	}
}

// collectTransitive performs BFS to collect all transitively reachable classes.
func (r *Reasoner) collectTransitive(start ClassIRI, index map[ClassIRI][]ClassIRI) []ClassIRI {
	visited := make(map[ClassIRI]bool)
	var result []ClassIRI
	queue := []ClassIRI{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range index[cur] {
			if !visited[next] {
				visited[next] = true
				result = append(result, next)
				queue = append(queue, next)
			}
		}
	}
	return result
}

// Subsumes returns true if class 'a' subsumes class 'b' (b is a subclass of a).
func (r *Reasoner) Subsumes(a, b ClassIRI) bool {
	if a == b {
		return true
	}
	for _, sup := range r.allSuperClasses[b] {
		if sup == a {
			return true
		}
	}
	return false
}

// AllSubClasses returns all transitive subclasses of the given class.
func (r *Reasoner) AllSubClasses(cls ClassIRI) []ClassIRI {
	return r.allSubClasses[cls]
}

// AllSuperClasses returns all transitive superclasses of the given class.
func (r *Reasoner) AllSuperClasses(cls ClassIRI) []ClassIRI {
	return r.allSuperClasses[cls]
}

// DirectSubClasses returns only the direct (non-transitive) subclasses.
func (r *Reasoner) DirectSubClasses(cls ClassIRI) []ClassIRI {
	return r.subClassIndex[cls]
}

// DirectSuperClasses returns only the direct (non-transitive) superclasses.
func (r *Reasoner) DirectSuperClasses(cls ClassIRI) []ClassIRI {
	return r.superClassIndex[cls]
}

// AncestorTypes returns all types that should be materialized for a given type,
// including the type itself. This is the core function used by the write-time materializer.
func (r *Reasoner) AncestorTypes(cls ClassIRI) []ClassIRI {
	result := []ClassIRI{cls}
	result = append(result, r.allSuperClasses[cls]...)
	return result
}

// InverseProperty returns the inverse of the given property, if declared.
func (r *Reasoner) InverseProperty(prop PropertyIRI) (PropertyIRI, bool) {
	if op, ok := r.ont.ObjectProperties[prop]; ok && op.InverseOf != "" {
		return op.InverseOf, true
	}
	return "", false
}

// IsTransitive returns whether the given property is declared as transitive.
func (r *Reasoner) IsTransitive(prop PropertyIRI) bool {
	if op, ok := r.ont.ObjectProperties[prop]; ok {
		return op.Characteristics.Transitive
	}
	return false
}

// IsSymmetric returns whether the given property is declared as symmetric.
func (r *Reasoner) IsSymmetric(prop PropertyIRI) bool {
	if op, ok := r.ont.ObjectProperties[prop]; ok {
		return op.Characteristics.Symmetric
	}
	return false
}

// IsFunctional returns whether the given property is declared as functional.
func (r *Reasoner) IsFunctional(prop PropertyIRI) bool {
	if op, ok := r.ont.ObjectProperties[prop]; ok {
		return op.Characteristics.Functional
	}
	if dp, ok := r.ont.DataProperties[prop]; ok {
		return dp.IsFunctional
	}
	return false
}

// InferredTypes returns the types that can be inferred for a subject/object
// based on domain/range declarations for the given property.
func (r *Reasoner) InferredTypes(prop PropertyIRI, isSubject bool) []ClassIRI {
	if op, ok := r.ont.ObjectProperties[prop]; ok {
		if isSubject {
			return op.Domain
		}
		return op.Range
	}
	if dp, ok := r.ont.DataProperties[prop]; ok && isSubject {
		return dp.Domain
	}
	return nil
}
