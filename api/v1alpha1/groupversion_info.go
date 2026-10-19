// Package v1alpha1 contains API Schema definitions for the novanet v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=novanet.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Builder collects the types of one API group-version and adds them to a
// runtime.Scheme. It replaces controller-runtime's scheme.Builder, which is
// deprecated for API packages (they should depend only on apimachinery).
type Builder struct {
	GroupVersion schema.GroupVersion
	runtime.SchemeBuilder
}

// Register queues the given objects for registration under the group-version.
func (b *Builder) Register(objs ...runtime.Object) *Builder {
	b.SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(b.GroupVersion, objs...)
		metav1.AddToGroupVersion(s, b.GroupVersion)
		return nil
	})
	return b
}

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "novanet.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
