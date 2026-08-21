package v1alpha1

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// namePattern constrains the cube identity: DNS-label-like, max 31
// chars. The error message derives from it — one source, no drift.
const namePattern = `^[a-z0-9][a-z0-9-]{0,30}$`

var nameRE = regexp.MustCompile(namePattern)

// Validate checks the defaulted Config and returns every problem found.
// It never mutates; call Default first.
func (c *Config) Validate() field.ErrorList {
	var errs field.ErrorList
	namePath := field.NewPath("metadata", "name")

	switch {
	case c.Name == "":
		errs = append(errs, field.Required(namePath, "cube identity is required"))
	case !nameRE.MatchString(c.Name):
		errs = append(errs, field.Invalid(namePath, c.Name,
			fmt.Sprintf("must match %s (lowercase alphanumeric and dashes, max 31 chars)", namePattern)))
	}
	if c.Spec.Cluster != nil && c.Spec.Cluster.Provider != ClusterProviderKind {
		errs = append(errs, field.NotSupported(
			field.NewPath("spec", "cluster", "provider"),
			string(c.Spec.Cluster.Provider), []string{string(ClusterProviderKind)}))
	}
	if c.Spec.Engine != nil {
		if c.Spec.Engine.Provider != EngineProviderFlux {
			errs = append(errs, field.NotSupported(
				field.NewPath("spec", "engine", "provider"),
				string(c.Spec.Engine.Provider), []string{string(EngineProviderFlux)}))
		}
		if c.Spec.Engine.Source != nil {
			errs = append(errs, validateEngineSource(c.Spec.Engine.Source)...)
		}
	}
	errs = append(errs, validatePacks(c.Spec.Packs)...)
	return errs
}

// validateEngineSource checks the discriminated engine source: a known kind, a
// URL whose scheme matches the kind, and a parseable interval.
func validateEngineSource(s *EngineSource) field.ErrorList {
	var errs field.ErrorList
	base := field.NewPath("spec", "engine", "source")

	switch s.Kind {
	case EngineSourceGit, EngineSourceOCI:
	default:
		errs = append(errs, field.NotSupported(base.Child("kind"), string(s.Kind),
			[]string{string(EngineSourceGit), string(EngineSourceOCI)}))
	}

	switch {
	case s.URL == "":
		errs = append(errs, field.Required(base.Child("url"), "a source URL is required"))
	case s.Kind == EngineSourceOCI && !strings.HasPrefix(s.URL, "oci://"):
		errs = append(errs, field.Invalid(base.Child("url"), s.URL, `kind "oci" requires an oci:// URL`))
	case s.Kind == EngineSourceGit && strings.HasPrefix(s.URL, "oci://"):
		errs = append(errs, field.Invalid(base.Child("url"), s.URL, `kind "git" must not use an oci:// URL`))
	}

	if s.Interval != "" {
		if _, err := time.ParseDuration(s.Interval); err != nil {
			errs = append(errs, field.Invalid(base.Child("interval"), s.Interval,
				"must be a valid duration (e.g. 10m, 1h)"))
		}
	}
	return errs
}
