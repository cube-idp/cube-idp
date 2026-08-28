package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true

// Config is the cube-idp configuration document.
// Only metadata.name is honored from ObjectMeta; server-populated fields
// (uid, resourceVersion, ...) are accepted and ignored when file-loaded.
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ConfigSpec `json:"spec,omitempty"`
}

// ConfigSpec grows one typed sub-struct per component domain, together
// with its defaults and validation — the loading machinery never changes.
// A sub-struct may also carry a cross-component surface owned by one
// component's contract instead of being named for a component:
// Prerequisites is the gateway component's (M11-A0, docs/ARCHITECTURE.md §3).
type ConfigSpec struct {
	// Cluster is optional: a config without it is valid (config-only use);
	// `init` requires it and fails with CUBE-CLU-001 when absent.
	Cluster *ClusterSpec `json:"cluster,omitempty"`

	// Engine is optional: an absent spec.engine is treated by bootstrap as
	// the Flux default (the engine is mandatory). When present it selects
	// or pins the gitops engine cube-idp bootstraps.
	Engine *EngineSpec `json:"engine,omitempty"`

	// Gateway is optional: an absent spec.gateway means the trust-fabric
	// gateway is installed with defaults. It is fundamental fabric, not
	// opt-in — the engine precedent — so absence configures, never disables.
	Gateway *GatewaySpec `json:"gateway,omitempty"`

	// CA is optional: an absent spec.ca means the "cube" provider — the
	// cube-owned, stdlib-minted certificate authority.
	CA *CASpec `json:"ca,omitempty"`

	// Prerequisites is the ordered list of units bootstrap installs ahead
	// of the engine. Absent or empty means the compiled defaults; a
	// present, non-empty list replaces them entirely — what is written is
	// exactly what runs, in list order. A slice rather than a pointer:
	// absent and empty mean the same thing here, so there is nothing to
	// distinguish. The model — which names are built-in, what each unit
	// installs — is the gateway domain's contract
	// (docs/domains/gateway.md).
	Prerequisites []PrerequisiteSpec `json:"prerequisites,omitempty"`

	// Packs are the pack instances in the setup; absent means none are
	// managed. A slice rather than a pointer: an empty list and an absent
	// list mean the same thing here, so there is nothing to distinguish.
	Packs []PackSpec `json:"packs,omitempty"`
}
