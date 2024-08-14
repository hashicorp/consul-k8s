// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestFinalizersPatcher(t *testing.T) {
	cases := []struct {
		name                   string
		oldObject              client.Object
		addFinalizers          []string
		removeFinalizers       []string
		expectedFinalizerPatch *FinalizerPatch
		op                     string
	}{
		{
			name: "adds finalizers at the end and keeps the original list in order",
			oldObject: &v1alpha1.ServiceResolver{
				ObjectMeta: v1.ObjectMeta{
					Finalizers: []string{
						"a",
						"b",
						"c",
					},
				},
			},
			addFinalizers: []string{"d", "e"},
			expectedFinalizerPatch: &FinalizerPatch{
				NewFinalizers: []string{"a", "b", "c", "d", "e"},
			},
		},
		{
			name: "adds finalizers when original list is empty",
			oldObject: &v1alpha1.ServiceResolver{
				ObjectMeta: v1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			addFinalizers: []string{"d", "e"},
			expectedFinalizerPatch: &FinalizerPatch{
				NewFinalizers: []string{"d", "e"},
			},
		},
		{
			name: "removes finalizers keeping the original list in order",
			oldObject: &v1alpha1.ServiceResolver{
				ObjectMeta: v1.ObjectMeta{
					Finalizers: []string{
						"a",
						"b",
						"c",
						"d",
					},
				},
			},
			removeFinalizers: []string{"b"},
			expectedFinalizerPatch: &FinalizerPatch{
				NewFinalizers: []string{"a", "c", "d"},
			},
		},
		{
			name: "removes all finalizers if specified",
			oldObject: &v1alpha1.ServiceResolver{
				ObjectMeta: v1.ObjectMeta{
					Finalizers: []string{
						"a",
						"b",
					},
				},
			},
			removeFinalizers: []string{"a", "b"},
			expectedFinalizerPatch: &FinalizerPatch{
				NewFinalizers: []string{},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := FinalizerPatcher{}
			var patch *FinalizerPatch

			if len(c.addFinalizers) > 0 {
				patch = f.AddFinalizersPatch(c.oldObject, c.addFinalizers...)
			} else if len(c.removeFinalizers) > 0 {
				patch = f.RemoveFinalizersPatch(c.oldObject, c.removeFinalizers...)
			}

			require.Equal(t, c.expectedFinalizerPatch, patch)
		})
	}
}
