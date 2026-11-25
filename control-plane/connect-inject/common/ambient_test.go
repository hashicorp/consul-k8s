// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestAmbientMeshEnabled(t *testing.T) {
	cases := map[string]struct {
		podAnno   map[string]string
		nsLabels  map[string]string
		feature   bool
		expected  bool
		expectErr bool
	}{
		"feature disabled ignores opt-in": {
			podAnno: map[string]string{constants.KeyAmbientMesh: "true"}, feature: false, expected: false,
		},
		"no opt-in": {feature: true, expected: false},
		"namespace label opt-in": {
			nsLabels: map[string]string{constants.KeyAmbientMesh: "true"}, feature: true, expected: true,
		},
		"pod annotation overrides namespace label": {
			podAnno:  map[string]string{constants.KeyAmbientMesh: "false"},
			nsLabels: map[string]string{constants.KeyAmbientMesh: "true"}, feature: true, expected: false,
		},
		"pod annotation opt-in": {
			podAnno: map[string]string{constants.KeyAmbientMesh: "true"}, feature: true, expected: true,
		},
		"invalid value": {
			podAnno: map[string]string{constants.KeyAmbientMesh: "yes-please"}, feature: true, expectErr: true,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns", Labels: c.nsLabels}}
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Annotations: c.podAnno}}
			got, err := AmbientMeshEnabled(ns, pod, c.feature)
			if c.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.expected, got)
		})
	}
}
