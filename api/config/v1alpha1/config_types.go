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

// ConfigSpec is intentionally minimal in v0. Each component domain adds
// one typed sub-struct here (Cluster, Engine, Packs, ...) together with
// its defaults and validation — the loading machinery never changes.
type ConfigSpec struct{}
