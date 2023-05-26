// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureFinalizer(t *testing.T) {
	t.Parallel()

	finalizer := "test-finalizer"

	cases := map[string]struct {
		initialFinalizers []string
		finalizerToAdd    string
		expectedDidUpdate bool
	}{
		"should update":     {[]string{}, finalizer, true},
		"should not update": {[]string{finalizer}, finalizer, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// It doesn't matter what the object is, as long as it implements client.Object.
			// A Pod was as good as any other object here.
			testObj := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-obj",
					Finalizers: tc.initialFinalizers,
				},
			}

			client := fake.NewClientBuilder().WithObjects(testObj).Build()

			didUpdate, err := EnsureFinalizer(context.Background(), client, testObj, tc.finalizerToAdd)

			require.NoError(t, err)
			require.Equal(t, tc.expectedDidUpdate, didUpdate)
		})
	}
}

func TestRemoveFinalizer(t *testing.T) {
	t.Parallel()

	finalizer := "test-finalizer"

	cases := map[string]struct {
		initialFinalizers []string
		finalizerToRemove string
		expectedDidUpdate bool
	}{
		"should update":     {[]string{finalizer}, finalizer, true},
		"should not update": {[]string{}, finalizer, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// It doesn't matter what the object is, as long as it implements client.Object.
			// A Pod was as good as any other object here.
			testObj := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-obj",
					Finalizers: tc.initialFinalizers,
				},
			}

			client := fake.NewClientBuilder().WithObjects(testObj).Build()

			didUpdate, err := RemoveFinalizer(context.Background(), client, testObj, tc.finalizerToRemove)

			require.NoError(t, err)
			require.Equal(t, tc.expectedDidUpdate, didUpdate)
		})
	}
}
