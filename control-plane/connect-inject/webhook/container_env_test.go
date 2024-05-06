// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContainerEnvVars(t *testing.T) {

	cases := []struct {
		Name     string
		Upstream string
		required []corev1.EnvVar
	}{
		{
			"Upstream with datacenter",
			"static-server:7890:dc1",
			[]corev1.EnvVar{
				{
					Name:  "STATIC_SERVER_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER_CONNECT_SERVICE_PORT",
					Value: "7890",
				},
			},
		},
		{
			"Upstream without datacenter",
			"static-server:7890",
			[]corev1.EnvVar{
				{
					Name:  "STATIC_SERVER_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER_CONNECT_SERVICE_PORT",
					Value: "7890",
				},
			},
		},
		{
			"Multiple upstreams comma separated",
			"static-server:7890, static-server2:7892",
			[]corev1.EnvVar{
				{
					Name:  "STATIC_SERVER_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER_CONNECT_SERVICE_PORT",
					Value: "7890",
				},
				{
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_PORT",
					Value: "7892",
				},
			},
		},
		{
			"Multiple upstreams comma separated",
			"static-server:7890, static-server2:7892 static-server3:7893",
			[]corev1.EnvVar{
				{
					Name:  "STATIC_SERVER_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER_CONNECT_SERVICE_PORT",
					Value: "7890",
				},
				{
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_PORT",
					Value: "7892",
				},
				{
					Name:  "STATIC_SERVER3_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER3_CONNECT_SERVICE_PORT",
					Value: "7893",
				},
			},
		},
		{
			"Multiple upstreams comma separated and carriage return",
			`static-server:7890,
                       static-server2:7892
                       static-server3:7893`,
			[]corev1.EnvVar{
				{
					Name:  "STATIC_SERVER_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER_CONNECT_SERVICE_PORT",
					Value: "7890",
				},
				{
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_PORT",
					Value: "7892",
				},
				{
					Name:  "STATIC_SERVER3_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER3_CONNECT_SERVICE_PORT",
					Value: "7893",
				},
			},
		},
		{
			"Multiple upstreams comma separated and carriage return malformed upstream",
			`static-server7890,
                       static-server2:7892
static-server3:7893`,
			[]corev1.EnvVar{
				{
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER2_CONNECT_SERVICE_PORT",
					Value: "7892",
				},
				{
					Name:  "STATIC_SERVER3_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER3_CONNECT_SERVICE_PORT",
					Value: "7893",
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var w MeshWebhook
			envVars := w.containerEnvVars(corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:   "foo",
						constants.AnnotationUpstreams: tt.Upstream,
					},
				},
			})

			require.ElementsMatch(envVars, tt.required)
		})
	}
}
