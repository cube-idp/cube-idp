package v1alpha1

import (
	"fmt"
	"regexp"

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
	if c.Spec.Engine != nil && c.Spec.Engine.Provider != EngineProviderFlux {
		errs = append(errs, field.NotSupported(
			field.NewPath("spec", "engine", "provider"),
			string(c.Spec.Engine.Provider), []string{string(EngineProviderFlux)}))
	}
	return errs
}
