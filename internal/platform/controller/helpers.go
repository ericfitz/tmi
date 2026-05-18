package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Package-local pointer helpers. Kubernetes API types take pointers for
// optional scalar fields; these one-liners keep the render functions
// readable. Consolidated here so new render_*.go files reuse them instead
// of redeclaring near-identical helpers.

func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }

func intOrStringPtr(v intstr.IntOrString) *intstr.IntOrString { return &v }

func protoPtr(p corev1.Protocol) *corev1.Protocol { return &p }
