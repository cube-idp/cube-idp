// Package v1alpha1 contains the cube-idp.dev/v1alpha1 API types.
//
// The Config kind is KRM-shaped: loaded from a local file today, written
// to Kubernetes API conventions so it can be served as a CRD later with
// no type changes. This package is a pure contract: types, defaults, and
// validation only — no I/O (the loader lives in internal/config).
//
// +kubebuilder:object:generate=true
// +groupName=cube-idp.dev
package v1alpha1

//go:generate go tool controller-gen object paths=.
