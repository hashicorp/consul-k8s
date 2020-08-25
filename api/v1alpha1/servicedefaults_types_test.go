package v1alpha1

import (
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRun_ToConsul(t *testing.T) {
	cases := []ToConsulCase{
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-config",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
					MeshGateway: MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
		},
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-config",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:    80,
								Path:            "/test/path",
								LocalPathPort:   42,
								Protocol:        "tcp",
								ParsedFromCheck: true,
							},
						},
					},
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:    80,
							Path:            "/test/path",
							LocalPathPort:   42,
							Protocol:        "tcp",
							ParsedFromCheck: true,
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
		},
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-configs",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:    80,
								Path:            "/test/path",
								LocalPathPort:   42,
								Protocol:        "tcp",
								ParsedFromCheck: true,
							},
							{
								ListenerPort:    8080,
								Path:            "/second/test/path",
								LocalPathPort:   11,
								Protocol:        "https",
								ParsedFromCheck: false,
							},
						},
					},
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:    80,
							Path:            "/test/path",
							LocalPathPort:   42,
							Protocol:        "tcp",
							ParsedFromCheck: true,
						},
						{
							ListenerPort:    8080,
							Path:            "/second/test/path",
							LocalPathPort:   11,
							Protocol:        "https",
							ParsedFromCheck: false,
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
		},
	}

	for _, testCase := range cases {
		output := testCase.input.ToConsul()
		require.Equal(t, testCase.expected, output)
	}
}

func TestRun_MatchesConsul(t *testing.T) {
	cases := []MatchesConsulCase{
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-config",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
					MeshGateway: MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
			true,
		},
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-config",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeDefault,
				},
			},
			true,
		},
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-config",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:    80,
								Path:            "/test/path",
								LocalPathPort:   42,
								Protocol:        "tcp",
								ParsedFromCheck: true,
							},
						},
					},
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:    80,
							Path:            "/test/path",
							LocalPathPort:   42,
							Protocol:        "tcp",
							ParsedFromCheck: true,
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			true,
		},
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-configs",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:    80,
								Path:            "/test/path",
								LocalPathPort:   42,
								Protocol:        "tcp",
								ParsedFromCheck: true,
							},
							{
								ListenerPort:    8080,
								Path:            "/second/test/path",
								LocalPathPort:   11,
								Protocol:        "https",
								ParsedFromCheck: false,
							},
						},
					},
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:    80,
							Path:            "/test/path",
							LocalPathPort:   42,
							Protocol:        "tcp",
							ParsedFromCheck: true,
						},
						{
							ListenerPort:    8080,
							Path:            "/second/test/path",
							LocalPathPort:   11,
							Protocol:        "https",
							ParsedFromCheck: false,
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			true,
		},
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-configs",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:    8080,
								Path:            "/second/test/path",
								LocalPathPort:   11,
								Protocol:        "https",
								ParsedFromCheck: false,
							},
							{
								ListenerPort:    80,
								Path:            "/test/path",
								LocalPathPort:   42,
								Protocol:        "tcp",
								ParsedFromCheck: true,
							},
						},
					},
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:    8080,
							Path:            "/second/test/path",
							LocalPathPort:   11,
							Protocol:        "https",
							ParsedFromCheck: false,
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			false,
		},
		{
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-configs",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:    8080,
								Path:            "/second/test/path",
								LocalPathPort:   11,
								Protocol:        "https",
								ParsedFromCheck: false,
							},
							{
								ListenerPort:    80,
								Path:            "/test/path",
								LocalPathPort:   42,
								Protocol:        "tcp",
								ParsedFromCheck: true,
							},
						},
					},
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:    80,
							Path:            "/test/path",
							LocalPathPort:   42,
							Protocol:        "tcp",
							ParsedFromCheck: true,
						},
						{
							ListenerPort:    8080,
							Path:            "/second/test/path",
							LocalPathPort:   11,
							Protocol:        "https",
							ParsedFromCheck: false,
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			true,
		},
	}

	for _, testCase := range cases {
		result := testCase.internal.MatchesConsul(testCase.consul)
		require.Equal(t, testCase.matches, result)
	}
}

type ToConsulCase struct {
	input    *ServiceDefaults
	expected *capi.ServiceConfigEntry
}

type MatchesConsulCase struct {
	internal *ServiceDefaults
	consul   *capi.ServiceConfigEntry
	matches  bool
}
