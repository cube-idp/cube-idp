package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the group and version of this API: cube-idp.dev/v1alpha1.
var GroupVersion = schema.GroupVersion{Group: "cube-idp.dev", Version: "v1alpha1"}

var (
	// SchemeBuilder collects functions that register this package's types.
	// Unused while configs are file-loaded; required for CRD promotion.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme registers this package's types into a runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &Config{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
