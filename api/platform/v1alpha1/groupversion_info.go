// Package v1alpha1 contains the TMIComponent custom resource API for the
// TMI Component Platform.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is the group/version for the TMI Component Platform API.
var GroupVersion = schema.GroupVersion{Group: "tmi.dev", Version: "v1alpha1"}

// SchemeBuilder registers the API types into a runtime scheme.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

// AddToScheme adds the types in this group/version to a scheme.
var AddToScheme = SchemeBuilder.AddToScheme
