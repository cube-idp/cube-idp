package v1alpha1

import (
	"regexp"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// nameRE constrains the cube identity: DNS-label-like, max 31 chars.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

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
			"must match ^[a-z0-9][a-z0-9-]{0,30}$ (lowercase alphanumeric and dashes, max 31 chars)"))
	}
	if c.Spec.Cluster != nil && c.Spec.Cluster.Provider != ClusterProviderKind {
		errs = append(errs, field.NotSupported(
			field.NewPath("spec", "cluster", "provider"),
			string(c.Spec.Cluster.Provider), []string{string(ClusterProviderKind)}))
	}
	return errs
}
