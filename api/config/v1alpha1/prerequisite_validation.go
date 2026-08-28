package v1alpha1

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// validatePrerequisites checks spec.prerequisites at the document layer:
// presence and shape, and nothing more.
//
// Resolution is deliberately absent. Whether a ref resolves is internal/ref's
// answer (CUBE-REF-*) and whether an override pack loads, validates, and
// renders is the pack contract's (CUBE-PKG-*); this layer never performs I/O.
// Order and inter-unit dependency are the list author's — there is no
// dependency graph and no required-unit floor.
func validatePrerequisites(units []PrerequisiteSpec) field.ErrorList {
	var errs field.ErrorList
	base := field.NewPath("spec", "prerequisites")

	// Only a usable name can collide; one that fails the required check gets
	// that error alone, never a duplicate on top of it.
	seen := map[string]int{}

	for i, u := range units {
		path := base.Index(i)
		errs = append(errs, validatePrerequisite(path, u)...)

		if strings.TrimSpace(u.Name) == "" {
			continue
		}
		if first, dup := seen[u.Name]; dup {
			errs = append(errs, field.Duplicate(path.Child("name"),
				fmt.Sprintf("%s (already used by spec.prerequisites[%d])", u.Name, first)))
			continue
		}
		seen[u.Name] = i
	}
	return errs
}

func validatePrerequisite(path *field.Path, u PrerequisiteSpec) field.ErrorList {
	var errs field.ErrorList

	if u.Ref != "" {
		errs = append(errs, validateRefToken(path.Child("ref"), u.Ref)...)
	}

	// Without a name the entry's class is undecidable, so the ref rules below
	// would report noise on top of the real problem.
	if strings.TrimSpace(u.Name) == "" {
		return append(errs, field.Required(path.Child("name"),
			"a prerequisite unit name is required"))
	}

	switch {
	case isBuiltInPrerequisite(u.Name) && u.Ref != "":
		errs = append(errs, field.Forbidden(path.Child("ref"),
			fmt.Sprintf("%q is a built-in unit: its content is cube-owned, so there is nothing to reference", u.Name)))
	case !isWellKnownPrerequisite(u.Name) && u.Ref == "":
		errs = append(errs, field.Required(path.Child("ref"),
			fmt.Sprintf("%q is not a cube-shipped unit, so it must reference a pack", u.Name)))
	}
	return errs
}
