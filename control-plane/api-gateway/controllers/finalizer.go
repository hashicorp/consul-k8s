// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureFinalizer ensures that the given object has the given finalizer.
func EnsureFinalizer(ctx context.Context, client client.Client, object client.Object, finalizer string) (didUpdate bool, err error) {
	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == finalizer {
			return false, nil
		}
	}
	object.SetFinalizers(append(finalizers, finalizer))
	if err := client.Update(ctx, object); err != nil {
		return false, err
	}

	return true, nil
}

// RemoveFinalizer removes the given finalizer from the given object.
func RemoveFinalizer(ctx context.Context, client client.Client, object client.Object, finalizer string) (didUpdate bool, err error) {
	finalizers := object.GetFinalizers()

	for i, f := range finalizers {
		if f == finalizer {
			finalizers = append(finalizers[:i], finalizers[i+1:]...)
			object.SetFinalizers(finalizers)
			if err := client.Update(ctx, object); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	return false, nil
}
