package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Package-local pointer helpers. Kubernetes API types take pointers for
// optional scalar fields; these one-liners keep the render functions
// readable. Consolidated here so new render_*.go files reuse them instead
// of redeclaring near-identical helpers.

// SEM@033363404fa8d485d9d46c5454abf63fe8bfc1e5: convert a bool value to a pointer for Kubernetes API optional fields (pure)
func boolPtr(b bool) *bool { return &b }

// SEM@033363404fa8d485d9d46c5454abf63fe8bfc1e5: convert an int64 value to a pointer for Kubernetes API optional fields (pure)
func int64Ptr(i int64) *int64 { return &i }

// SEM@033363404fa8d485d9d46c5454abf63fe8bfc1e5: convert an IntOrString value to a pointer for Kubernetes API optional fields (pure)
func intOrStringPtr(v intstr.IntOrString) *intstr.IntOrString { return &v }

// SEM@033363404fa8d485d9d46c5454abf63fe8bfc1e5: convert a Protocol value to a pointer for Kubernetes API optional fields (pure)
func protoPtr(p corev1.Protocol) *corev1.Protocol { return &p }
