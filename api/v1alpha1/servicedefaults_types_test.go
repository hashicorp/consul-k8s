package v1alpha1

import (
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_ToConsul(t *testing.T) {
	cases := map[string]struct {
		input    *ServiceDefaults
		expected *capi.ServiceConfigEntry
	}{
		"protocol:http,mode:remote": {
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
		"protocol:https,mode:local,exposePaths:1,externalSNI": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
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
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
		},
		"protocol:\"\",mode:\"\",exposePaths:2,externalSNI": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-configs",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "",
					MeshGateway: MeshGatewayConfig{
						Mode: "",
					},
					Expose: ExposeConfig{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/second/test/path",
								LocalPathPort: 11,
								Protocol:      "https",
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
				Protocol:  "",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeDefault,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/second/test/path",
							LocalPathPort: 11,
							Protocol:      "https",
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
		},
		"protocol:http,mode:none,exposePaths:1,exposeChecks:false": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-configs",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
					MeshGateway: MeshGatewayConfig{
						Mode: "none",
					},
					Expose: ExposeConfig{
						Checks: false,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeNone,
				},
				Expose: capi.ExposeConfig{
					Checks: false,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
					},
				},
			},
		},
		"protocol:https,mode:unsupported": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-test-service",
					Namespace: "consul-configs",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGatewayConfig{
						Mode: "unsupported",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeDefault,
				},
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			output := testCase.input.ToConsul()
			require.Equal(t, testCase.expected, output)
		})
	}
}

func Test_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		internal *ServiceDefaults
		consul   *capi.ServiceConfigEntry
		matches  bool
	}{
		"name:matches": {
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
		"name:mismatched": {
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
				Name:      "differently-named-service",
				Namespace: "",
				Protocol:  "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
			false,
		},
		"protocol:matches": {
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
		"protocol:mismatched": {
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
				Protocol:  "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeDefault,
				},
			},
			false,
		},
		"gatewayConfig:matches": {
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
		"gatewayConfig:mismatched": {
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
					Mode: capi.MeshGatewayModeLocal,
				},
			},
			false,
		},
		"externalSNI:matches": {
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
					ExternalSNI: "test-external-sni",
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
				ExternalSNI: "test-external-sni",
			},
			true,
		},
		"externalSNI:mismatched": {
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
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:      capi.ServiceDefaults,
				Name:      "my-test-service",
				Namespace: "",
				Protocol:  "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				ExternalSNI: "different-external-sni",
			},
			false,
		},
		"expose.checks:matches": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
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
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			true,
		},
		"expose.checks:mismatched": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
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
					Checks: false,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			false,
		},
		"expose.paths:matches": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/second/test/path",
								LocalPathPort: 11,
								Protocol:      "https",
							},
						},
					},
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
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/second/test/path",
							LocalPathPort: 11,
							Protocol:      "https",
						},
					},
				},
			},
			true,
		},
		"expose.paths.listenerPort:mismatched": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
						},
					},
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
							ListenerPort:  81,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
					},
				},
			},
			false,
		},
		"expose.paths.path:mismatched": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
						},
					},
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
							ListenerPort:  80,
							Path:          "/differnt/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
					},
				},
			},
			false,
		},
		"expose.paths.localPathPort:mismatched": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
						},
					},
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
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 21,
							Protocol:      "tcp",
						},
					},
				},
			},
			false,
		},
		"expose.paths.protocol:mismatched": {
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
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
						},
					},
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
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "https",
						},
					},
				},
			},
			false,
		},
		"expose.paths:mismatched when path lengths are different": {
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
								ListenerPort:  8080,
								Path:          "/second/test/path",
								LocalPathPort: 11,
								Protocol:      "https",
							},
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
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
							ListenerPort:  8080,
							Path:          "/second/test/path",
							LocalPathPort: 11,
							Protocol:      "https",
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			false,
		},
		"expose.paths:match when paths orders are different": {
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
								ListenerPort:  8080,
								Path:          "/second/test/path",
								LocalPathPort: 11,
								Protocol:      "https",
							},
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
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
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/second/test/path",
							LocalPathPort: 11,
							Protocol:      "https",
						},
					},
				},
				ExternalSNI: "test-external-sni",
			},
			true,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			result := testCase.internal.MatchesConsul(testCase.consul)
			require.Equal(t, testCase.matches, result)
		})
	}
}
