package v1alpha1

import (
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestToConsul(t *testing.T) {
	cases := map[string]struct {
		input    *ServiceDefaults
		expected *capi.ServiceConfigEntry
	}{
		"kind:service-defaults": {
			&ServiceDefaults{},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
			},
		},
		"name:resource-name": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-name",
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Name: "resource-name",
			},
		},
		"protocol:http": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Protocol: "http",
			},
		},
		"protocol:https": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Protocol: "https",
			},
		},
		"protocol:''": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Protocol: "",
			},
		},
		"mode:unsupported": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "unsupported",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeDefault,
				},
			},
		},
		"mode:local": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
			},
		},
		"mode:remote": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
		},
		"mode:none": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "none",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeNone,
				},
			},
		},
		"mode:default": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "default",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeDefault,
				},
			},
		},
		"mode:''": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeDefault,
				},
			},
		},
		"externalSNI:test-external-sni": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				ExternalSNI: "test-external-sni",
			},
		},
		"externalSNI:''": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					ExternalSNI: "",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				ExternalSNI: "",
			},
		},
		"expose.checks:false": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Checks: false,
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Checks: false,
				},
			},
		},
		"expose.checks:true": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Checks: true,
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Checks: true,
				},
			},
		},
		"expose.paths:single": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
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
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
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
		"expose.paths:multiple": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/root/test/path",
								LocalPathPort: 4201,
								Protocol:      "https",
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/root/test/path",
							LocalPathPort: 4201,
							Protocol:      "https",
						},
					},
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

func TestMatchesConsul(t *testing.T) {
	cases := map[string]struct {
		internal *ServiceDefaults
		consul   *capi.ServiceConfigEntry
		matches  bool
	}{
		"name:matches": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Name: "my-test-service",
			},
			true,
		},
		"name:mismatched": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Name: "differently-named-service",
			},
			false,
		},
		"protocol:matches": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Protocol: "http",
			},
			true,
		},
		"protocol:mismatched": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Protocol: "https",
			},
			false,
		},
		"gatewayConfig:matches": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
			true,
		},
		"gatewayConfig:mismatched": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
			},
			false,
		},
		"externalSNI:matches": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				ExternalSNI: "test-external-sni",
			},
			true,
		},
		"externalSNI:mismatched": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					ExternalSNI: "test-external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				ExternalSNI: "different-external-sni",
			},
			false,
		},
		"expose.checks:matches": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Checks: true,
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Checks: true,
				},
			},
			true,
		},
		"expose.checks:mismatched": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Checks: true,
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Checks: false,
				},
			},
			false,
		},
		"expose.paths:matches": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
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
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
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
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								ListenerPort: 80,
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							ListenerPort: 81,
						},
					},
				},
			},
			false,
		},
		"expose.paths.path:mismatched": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								Path: "/test/path",
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							Path: "/differnt/path",
						},
					},
				},
			},
			false,
		},
		"expose.paths.localPathPort:mismatched": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								LocalPathPort: 42,
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							LocalPathPort: 21,
						},
					},
				},
			},
			false,
		},
		"expose.paths.protocol:mismatched": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								Protocol: "tcp",
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							Protocol: "https",
						},
					},
				},
			},
			false,
		},
		"expose.paths:mismatched when path lengths are different": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
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
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							ListenerPort:  8080,
							Path:          "/second/test/path",
							LocalPathPort: 11,
							Protocol:      "https",
						},
					},
				},
			},
			false,
		},
		"expose.paths:match when paths orders are different": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Expose: ExposeConfig{
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
				},
			},
			&capi.ServiceConfigEntry{
				Kind: capi.ServiceDefaults,
				Expose: capi.ExposeConfig{
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
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			result := testCase.internal.MatchesConsul(testCase.consul)
			require.Equal(t, testCase.matches, result)
		})
	}
}

func TestDefault(t *testing.T) {
	cases := map[string]struct {
		input    *ServiceDefaults
		expected *ServiceDefaults
	}{
		"protocol": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "",
				},
			},
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "tcp",
				},
			},
		},
		"expose.path.protocol": {
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "tcp",
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								Protocol: "",
							},
						},
					},
				},
			},
			&ServiceDefaults{
				Spec: ServiceDefaultsSpec{
					Protocol: "tcp",
					Expose: ExposeConfig{
						Paths: []ExposePath{
							{
								Protocol: "http",
							},
						},
					},
				},
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			testCase.input.Default()
			require.Equal(t, testCase.expected, testCase.input)
		})
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		input          *ServiceDefaults
		expectedErrMsg string
	}{
		"meshgateway.mode": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "foobar",
					},
				},
			},
			`ServiceDefaults.consul.hashicorp.com "my-service" is invalid: spec.meshGateway.mode: Invalid value: "foobar": must be on of "remote", "local", "none" or ""`,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate()
			require.EqualError(t, err, testCase.expectedErrMsg)
		})
	}
}
