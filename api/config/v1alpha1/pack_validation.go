package v1alpha1

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// validatePacks checks spec.packs at the document layer: everything decidable
// from the document alone, and nothing more.
//
// The rules that need a pack resolved — the effective ID, its uniqueness, and
// dependsOn targets — are deliberately absent. Deriving an effective ID needs
// the pack's own name, which means fetching it, and this layer never performs
// I/O. internal/pack owns those as CUBE-PKG-* once every pack is loaded.
func validatePacks(packs []PackSpec) field.ErrorList {
	var errs field.ErrorList
	base := field.NewPath("spec", "packs")

	// Only explicit IDs can collide at this layer; a defaulted one is not
	// known yet.
	seen := map[string]int{}

	for i, p := range packs {
		path := base.Index(i)
		errs = append(errs, validatePack(path, p)...)

		if p.ID == "" {
			continue
		}
		if first, dup := seen[p.ID]; dup {
			errs = append(errs, field.Duplicate(path.Child("id"),
				fmt.Sprintf("%s (already used by spec.packs[%d])", p.ID, first)))
			continue
		}
		seen[p.ID] = i
	}
	return errs
}

func validatePack(path *field.Path, p PackSpec) field.ErrorList {
	var errs field.ErrorList

	if p.ID != "" && !nameRE.MatchString(p.ID) {
		errs = append(errs, field.Invalid(path.Child("id"), p.ID,
			fmt.Sprintf("must match %s (lowercase alphanumeric and dashes, max 31 chars)", namePattern)))
	}

	if p.PackRef == "" {
		errs = append(errs, field.Required(path.Child("packRef"), "a pack reference is required"))
	} else {
		errs = append(errs, validateRefToken(path.Child("packRef"), p.PackRef)...)
	}
	if p.ValuesRef != "" {
		errs = append(errs, validateRefToken(path.Child("valuesRef"), p.ValuesRef)...)
	}

	for j, m := range p.ExternalManifests {
		errs = append(errs, validateExternalManifest(path.Child("externalManifests").Index(j), m)...)
	}

	// Only a self-reference by explicit ID is decidable here: any other
	// target needs the resolved set to say whether it exists.
	for j, dep := range p.DependsOn {
		depPath := path.Child("dependsOn").Index(j)
		switch {
		case strings.TrimSpace(dep) == "":
			errs = append(errs, field.Required(depPath, "a dependency target is required"))
		case p.ID != "" && dep == p.ID:
			errs = append(errs, field.Invalid(depPath, dep, "a pack cannot depend on itself"))
		}
	}
	return errs
}

func validateExternalManifest(path *field.Path, m ExternalManifest) field.ErrorList {
	var errs field.ErrorList

	switch {
	case m.Ref == "" && m.Manifest == nil:
		errs = append(errs, field.Required(path, "set exactly one of ref or manifest"))
	case m.Ref != "" && m.Manifest != nil:
		errs = append(errs, field.Invalid(path, "ref+manifest",
			"set exactly one of ref or manifest, not both"))
	case m.Ref != "":
		errs = append(errs, validateRefToken(path.Child("ref"), m.Ref)...)
	default:
		errs = append(errs, validateInlineManifest(path.Child("manifest"), m.Manifest)...)
	}

	switch m.Lifecycle {
	case LifecyclePre, LifecycleWith:
	default:
		errs = append(errs, field.NotSupported(path.Child("lifecycle"), string(m.Lifecycle),
			[]string{string(LifecyclePre), string(LifecycleWith)}))
	}
	return errs
}

// validateInlineManifest checks that an inline entry is one Kubernetes object
// — enough to be recognisable as such, which is what an author gets wrong.
// Whether the object is valid for its kind is the API server's business.
func validateInlineManifest(path *field.Path, raw *runtime.RawExtension) field.ErrorList {
	var errs field.ErrorList

	var obj struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := json.Unmarshal(raw.Raw, &obj); err != nil {
		return append(errs, field.Invalid(path, string(raw.Raw),
			"must be a single Kubernetes object"))
	}
	if obj.APIVersion == "" {
		errs = append(errs, field.Required(path.Child("apiVersion"), "an inline manifest needs apiVersion"))
	}
	if obj.Kind == "" {
		errs = append(errs, field.Required(path.Child("kind"), "an inline manifest needs kind"))
	}
	return errs
}

// validateRefToken checks that a reference is a well-formed token, and
// deliberately stops there.
//
// The reference grammar — which schemes exist and how each is spelled — is
// internal/ref's, and api/ can never import it. Restating a scheme table here
// would create a second grammar on the far side of a boundary that cannot be
// closed, which is exactly the duplicated reference handling this design set
// out to remove. A bad scheme is reported at resolution as CUBE-REF-*.
func validateRefToken(path *field.Path, ref string) field.ErrorList {
	for _, r := range ref {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return field.ErrorList{field.Invalid(path, ref,
				"must not contain whitespace or control characters")}
		}
	}
	return nil
}
