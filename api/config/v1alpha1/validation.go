package v1alpha1

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
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
		errs = append(errs, validateEngine(c.Spec.Engine)...)
	}
	if c.Spec.Gateway != nil {
		errs = append(errs, validateGateway(c.Spec.Gateway)...)
	}
	if c.Spec.CA != nil && c.Spec.CA.Provider != CAProviderCube {
		errs = append(errs, field.NotSupported(
			field.NewPath("spec", "ca", "provider"),
			string(c.Spec.CA.Provider), []string{string(CAProviderCube)}))
	}
	errs = append(errs, validatePacks(c.Spec.Packs)...)
	errs = append(errs, validatePrerequisites(c.Spec.Prerequisites)...)
	return errs
}

// validateGateway checks the gateway surface. An empty domain is not an
// error: Default derives it from a usable cube identity, and an unusable one
// is already reported at metadata.name.
func validateGateway(g *GatewaySpec) field.ErrorList {
	if g.Domain == "" {
		return nil
	}
	if msgs := validation.IsDNS1123Subdomain(g.Domain); len(msgs) > 0 {
		return field.ErrorList{field.Invalid(
			field.NewPath("spec", "gateway", "domain"), g.Domain,
			strings.Join(msgs, "; "))}
	}
	return nil
}

// validateEngine checks the engine surface: an admitted provider and, when
// present, its sync source.
func validateEngine(e *EngineSpec) field.ErrorList {
	var errs field.ErrorList

	if e.Provider != EngineProviderFlux {
		errs = append(errs, field.NotSupported(
			field.NewPath("spec", "engine", "provider"),
			string(e.Provider), []string{string(EngineProviderFlux)}))
	}
	if e.Source != nil {
		errs = append(errs, validateEngineSource(e.Source)...)
	}
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
