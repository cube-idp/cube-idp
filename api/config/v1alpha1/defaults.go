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
	if c.Spec.Gateway != nil {
		defaultGateway(c.Spec.Gateway, c.Name)
	}
	if c.Spec.CA != nil && c.Spec.CA.Provider == "" {
		c.Spec.CA.Provider = CAProviderCube
	}
	if len(c.Spec.Prerequisites) == 0 {
		c.Spec.Prerequisites = defaultPrerequisites()
	}
	for i := range c.Spec.Packs {
		defaultPack(&c.Spec.Packs[i])
	}
}

// defaultGateway derives the cube's base domain from the cube identity, and
// leaves it empty when that identity is not usable — metadata.name's own
// validation error is then the single truthful report, rather than a second
// error about a domain the user never wrote.
func defaultGateway(g *GatewaySpec, cubeName string) {
	if g.Domain != "" || !nameRE.MatchString(cubeName) {
		return
	}
	g.Domain = cubeName + "." + DefaultBaseDomain
}

// defaultPack fills a pack entry's optional fields. The effective ID is NOT
// defaulted here: it falls back to the pack's own name, which is only known
// once the pack is resolved — internal/pack derives it.
func defaultPack(p *PackSpec) {
	for i := range p.ExternalManifests {
		if p.ExternalManifests[i].Lifecycle == "" {
			p.ExternalManifests[i].Lifecycle = LifecycleWith
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
