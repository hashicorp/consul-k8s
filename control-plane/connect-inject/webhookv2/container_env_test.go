// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookv2

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestContainerEnvVars(t *testing.T) {
	t.Skip()
	// (TODO: ashwin) make these work once upstreams are fixed
	cases := []struct {
		Name     string
		Upstream string
	}{
		{
			"Upstream with datacenter",
			"static-server:7890:dc1",
		},
		{
			"Upstream without datacenter",
			"static-server:7890",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var w MeshWebhook
			envVars := w.containerEnvVars(corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:          "foo",
						constants.AnnotationMeshDestinations: tt.Upstream,
					},
				},
			})

			require.ElementsMatch(envVars, []corev1.EnvVar{
				{
					Name:  "STATIC_SERVER_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER_CONNECT_SERVICE_PORT",
					Value: "7890",
				},
			})
		})
	}
}
