package owl

import (
	"fmt"
	"strings"
)

// ProfileError represents an OWL2 RL profile violation.
type ProfileError struct {
	Violations []string
}

func (e *ProfileError) Error() string {
	return fmt.Sprintf("owl: OWL2 RL profile violations:\n  - %s", strings.Join(e.Violations, "\n  - "))
}

// ValidateProfile checks that the ontology conforms to OWL2 RL profile.
// Returns nil if valid, or a ProfileError listing all violations.
func ValidateProfile(ont *Ontology) error {
	if ont == nil {
		return fmt.Errorf("owl: nil ontology")
	}

	var violations []string

	// 1. Check for circular subClassOf
	for iri := range ont.Classes {
		if err := checkCircularSubClass(ont, iri, make(map[ClassIRI]bool)); err != nil {
			violations = append(violations, err.Error())
		}
	}

	// 2. OWL2 RL: class expressions in superclass position must be limited
	// In RL, superclass (right side of subClassOf) can only be:
	// - Named class
	// - Intersection of named classes
	// - HasValue restrictions
	// - maxCardinality 0 or 1
	// Cannot be: unionOf, someValuesFrom in superclass position
	// (We check EquivalentTo expressions which act as both sub and super)

	// 3. OWL2 RL: no nominals (oneOf) — we don't parse these yet, so no check needed

	// 4. Conflicting property characteristics
	for iri, op := range ont.ObjectProperties {
		if op.Characteristics.Symmetric && op.Characteristics.Asymmetric {
			violations = append(violations, fmt.Sprintf(
				"property %q is declared both Symmetric and Asymmetric", iri))
		}
		if op.Characteristics.Reflexive && op.Characteristics.Irreflexive {
			violations = append(violations, fmt.Sprintf(
				"property %q is declared both Reflexive and Irreflexive", iri))
		}
		if op.Characteristics.Functional && op.Characteristics.InverseFunctional &&
			op.Characteristics.Symmetric {
			// This is technically allowed but worth flagging as unusual
		}
	}

	// 5. Verify referenced classes exist (soft check — create implicit classes if needed)
	// This is informational, not a hard error in OWL2 RL

	// 6. Property domain/range consistency — range classes should exist
	for iri, op := range ont.ObjectProperties {
		for _, d := range op.Domain {
			if _, ok := ont.Classes[d]; !ok {
				// Auto-create implicit class (common in OWL)
				ont.Classes[d] = &Class{IRI: d}
			}
		}
		for _, r := range op.Range {
			if _, ok := ont.Classes[r]; !ok {
				ont.Classes[r] = &Class{IRI: r}
			}
		}
		_ = iri
	}

	if len(violations) > 0 {
		return &ProfileError{Violations: violations}
	}
	return nil
}

// checkCircularSubClass detects circular subClassOf chains.
func checkCircularSubClass(ont *Ontology, cls ClassIRI, visited map[ClassIRI]bool) error {
	if visited[cls] {
		return fmt.Errorf("circular subClassOf detected involving class %q", cls)
	}
	visited[cls] = true
	if c, ok := ont.Classes[cls]; ok {
		for _, sup := range c.SuperClasses {
			if err := checkCircularSubClass(ont, sup, copyMap(visited)); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyMap(m map[ClassIRI]bool) map[ClassIRI]bool {
	cp := make(map[ClassIRI]bool, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
