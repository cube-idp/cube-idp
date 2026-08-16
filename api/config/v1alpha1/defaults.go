package v1alpha1

// Default applies defaults in place. It is called by the loader after
// decoding and before Validate, and must be idempotent.
func (c *Config) Default() {
	if c.Spec.Cluster != nil && c.Spec.Cluster.Provider == "" {
		c.Spec.Cluster.Provider = ClusterProviderKind
	}
	if c.Spec.Engine != nil {
		if c.Spec.Engine.Provider == "" {
			c.Spec.Engine.Provider = EngineProviderFlux
		}
		if c.Spec.Engine.Source != nil {
			defaultEngineSource(c.Spec.Engine.Source)
		}
	}
}

// defaultEngineSource fills the engine source's optional fields, choosing the
// revision default by kind (git ⇒ main, oci ⇒ latest).
func defaultEngineSource(s *EngineSource) {
	if s.Kind == "" {
		s.Kind = EngineSourceGit
	}
	if s.Ref == "" {
		if s.Kind == EngineSourceOCI {
			s.Ref = "latest"
		} else {
			s.Ref = "main"
		}
	}
	if s.Path == "" {
		s.Path = "./"
	}
	if s.Interval == "" {
		s.Interval = "10m"
	}
}
