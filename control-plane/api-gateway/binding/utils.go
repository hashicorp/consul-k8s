// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// pointerTo is a convenience method for taking a pointer
// of an object without having to declare an intermediate variable.
// It's also useful for making sure we don't accidentally take
// the pointer of a range variable directly.
func pointerTo[T any](v T) *T {
	return &v
}

// isNil checks if the argument is nil. It's mainly used to
// check if a generic conforming to a nullable interface is
// actually nil.
func isNil(arg interface{}) bool {
	return arg == nil || reflect.ValueOf(arg).IsNil()
}

// bothNilOrEqual is used to determine if two pointers to comparable
// object are either nil or both point to the same value.
func bothNilOrEqual[T comparable](one, two *T) bool {
	if one == nil && two == nil {
		return true
	}
	if one == nil {
		return false
	}
	if two == nil {
		return false
	}
	return *one == *two
}

// valueOr checks if a string-like pointer is nil, and if it is,
// returns the given value instead.
func valueOr[T ~string](v *T, fallback string) string {
	if v == nil {
		return fallback
	}
	return string(*v)
}

// nilOrEqual checks if a string-like pointer is nil or if it is
// equal to the value provided.
func nilOrEqual[T ~string](v *T, check string) bool {
	return v == nil || string(*v) == check
}

// objectToMeta returns the NamespacedName for the given object.
func objectToMeta[T metav1.Object](object T) types.NamespacedName {
	return types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
}

// isDeleted checks if the deletion timestamp is set for an object.
func isDeleted(object client.Object) bool {
	return !object.GetDeletionTimestamp().IsZero()
}

// ensureFinalizer ensures that our finalizer is set on an object
// returning whether or not it modified the object.
func ensureFinalizer(object client.Object) bool {
	if !object.GetDeletionTimestamp().IsZero() {
		return false
	}

	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == gatewayFinalizer {
			return false
		}
	}

	object.SetFinalizers(append(finalizers, gatewayFinalizer))
	return true
}

// removeFinalizer ensures that our finalizer is absent from an object
// returning whether or not it modified the object.
func removeFinalizer(object client.Object) bool {
	found := false
	filtered := []string{}
	for _, f := range object.GetFinalizers() {
		if f == gatewayFinalizer {
			found = true
			continue
		}
		filtered = append(filtered, f)
	}

	object.SetFinalizers(filtered)
	return found
}
