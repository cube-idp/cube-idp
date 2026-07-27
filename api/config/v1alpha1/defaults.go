package v1alpha1

// Default applies defaults in place. It is called by the loader after
// decoding and before Validate, and must be idempotent.
//
// v0 has no defaultable fields yet; component sub-structs bring their own
// defaulting here as they are added to ConfigSpec.
func (c *Config) Default() {}
