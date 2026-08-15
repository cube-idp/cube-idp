package v1alpha1

// Default applies defaults in place. It is called by the loader after
// decoding and before Validate, and must be idempotent.
func (c *Config) Default() {
	if c.Spec.Cluster != nil && c.Spec.Cluster.Provider == "" {
		c.Spec.Cluster.Provider = ClusterProviderKind
	}
	if c.Spec.Engine != nil && c.Spec.Engine.Provider == "" {
		c.Spec.Engine.Provider = EngineProviderFlux
	}
}
