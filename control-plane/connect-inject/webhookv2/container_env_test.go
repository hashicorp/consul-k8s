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
	cases := []struct {
		Name        string
		Upstream    string
		ExpectError bool
	}{
		{
			// TODO: This will not error out when dcs are supported
			Name:        "Upstream with datacenter",
			Upstream:    "myPort.static-server:7890:dc1",
			ExpectError: true,
		},
		{
			Name:     "Upstream without datacenter",
			Upstream: "myPort.static-server:7890",
		},
		{
			// TODO: This will not error out when dcs are supported
			Name:        "Upstream with labels and datacenter",
			Upstream:    "myPort.port.static-server.svc.dc1.dc:7890",
			ExpectError: true,
		},
		{
			Name:     "Upstream with labels and no datacenter",
			Upstream: "myPort.port.static-server.svc:7890",
		},
		{
			Name:        "Error expected, wrong order",
			Upstream:    "static-server.svc.myPort.port:7890",
			ExpectError: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			var w MeshWebhook
			envVars, err := w.containerEnvVars(corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:          "foo",
						constants.AnnotationMeshDestinations: tt.Upstream,
					},
				},
			})

			if !tt.ExpectError {
				require.NoError(err)
				require.ElementsMatch(envVars, []corev1.EnvVar{
					{
						Name:  "STATIC_SERVER_MYPORT_CONNECT_SERVICE_HOST",
						Value: "127.0.0.1",
					}, {
						Name:  "STATIC_SERVER_MYPORT_CONNECT_SERVICE_PORT",
						Value: "7890",
					},
				})
			} else {
				require.Error(err)
			}
		})
	}
}
