package v1alpha1

import (
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/api/common"
)

func TestServiceDefaults_ToConsul(t *testing.T) {
	cases := map[string]struct {
		input    *ServiceDefaults
		expected *capi.ServiceConfigEntry
	}{
		"empty fields": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ServiceConfigEntry{
				Name: "foo",
				Kind: capi.ServiceDefaults,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGateway{
						Mode: "local",
					},
					Expose: Expose{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/path",
								LocalPathPort: 9000,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/another-path",
								LocalPathPort: 9091,
								Protocol:      "http2",
							},
						},
					},
					ExternalSNI: "external-sni",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     "foo",
				Protocol: "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/path",
							LocalPathPort: 9000,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/another-path",
							LocalPathPort: 9091,
							Protocol:      "http2",
						},
					},
				},
				ExternalSNI: "external-sni",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			output := testCase.input.ToConsul("datacenter")
			require.Equal(t, testCase.expected, output)
		})
	}
}

func TestServiceDefaults_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		internal *ServiceDefaults
		consul   capi.ConfigEntry
		matches  bool
	}{
		"empty fields matches": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				Name:        "my-test-service",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
			true,
		},
		"all fields populated matches": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
					MeshGateway: MeshGateway{
						Mode: "remote",
					},
					Expose: Expose{
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
					ExternalSNI: "sni-value",
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     "my-test-service",
				Protocol: "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
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
				ExternalSNI: "sni-value",
			},
			true,
		},
		"mismatched types does not match": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ProxyConfigEntry{
				Kind:        capi.ServiceDefaults,
				Name:        "my-test-service",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			false,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, testCase.matches, testCase.internal.MatchesConsul(testCase.consul))
		})
	}
}

func TestServiceDefaults_Validate(t *testing.T) {
	cases := map[string]struct {
		input          *ServiceDefaults
		expectedErrMsg string
	}{
		"valid": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGateway{
						Mode: "remote",
					},
					Expose: Expose{
						Checks: false,
						Paths: []ExposePath{
							{
								ListenerPort:  100,
								Path:          "/bar",
								LocalPathPort: 1000,
								Protocol:      "",
							},
						},
					},
				},
			},
			expectedErrMsg: "",
		},
		"meshgateway.mode": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGateway{
						Mode: "foobar",
					},
				},
			},
			`servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.meshGateway.mode: Invalid value: "foobar": must be one of "remote", "local", "none", ""`,
		},
		"expose.paths[].protocol": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					Expose: Expose{
						Paths: []ExposePath{
							{
								Protocol: "invalid-protocol",
								Path:     "/valid-path",
							},
						},
					},
				},
			},
			`servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.expose.paths[0].protocol: Invalid value: "invalid-protocol": must be one of "http", "http2"`,
		},
		"expose.paths[].path": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					Expose: Expose{
						Paths: []ExposePath{
							{
								Protocol: "http",
								Path:     "invalid-path",
							},
						},
					},
				},
			},
			`servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.expose.paths[0].path: Invalid value: "invalid-path": must begin with a '/'`,
		},
		"multi-error": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGateway{
						Mode: "invalid-mode",
					},
					Expose: Expose{
						Paths: []ExposePath{
							{
								Protocol: "invalid-protocol",
								Path:     "invalid-path",
							},
						},
					},
				},
			},
			`servicedefaults.consul.hashicorp.com "my-service" is invalid: [spec.meshGateway.mode: Invalid value: "invalid-mode": must be one of "remote", "local", "none", "", spec.expose.paths[0].path: Invalid value: "invalid-path": must begin with a '/', spec.expose.paths[0].protocol: Invalid value: "invalid-protocol": must be one of "http", "http2"]`,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(false)
			if testCase.expectedErrMsg != "" {
				require.EqualError(t, err, testCase.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestServiceDefaults_AddFinalizer(t *testing.T) {
	serviceDefaults := &ServiceDefaults{}
	serviceDefaults.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, serviceDefaults.ObjectMeta.Finalizers)
}

func TestServiceDefaults_RemoveFinalizer(t *testing.T) {
	serviceDefaults := &ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	serviceDefaults.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, serviceDefaults.ObjectMeta.Finalizers)
}

func TestServiceDefaults_SetSyncedCondition(t *testing.T) {
	serviceDefaults := &ServiceDefaults{}
	serviceDefaults.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, serviceDefaults.Status.Conditions[0].Status)
	require.Equal(t, "reason", serviceDefaults.Status.Conditions[0].Reason)
	require.Equal(t, "message", serviceDefaults.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, serviceDefaults.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceDefaults_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			serviceDefaults := &ServiceDefaults{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, serviceDefaults.SyncedConditionStatus())
		})
	}
}

func TestServiceDefaults_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ServiceDefaults{}).GetCondition(ConditionSynced))
}

func TestServiceDefaults_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ServiceDefaults{}).SyncedConditionStatus())
}

func TestServiceDefaults_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ServiceDefaults{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestServiceDefaults_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ServiceDefaults, (&ServiceDefaults{}).ConsulKind())
}

func TestServiceDefaults_KubeKind(t *testing.T) {
	require.Equal(t, "servicedefaults", (&ServiceDefaults{}).KubeKind())
}

func TestServiceDefaults_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceDefaults_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceDefaults_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&ServiceDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestServiceDefaults_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&ServiceDefaults{}).ConsulGlobalResource())
}

func TestServiceDefaults_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	serviceDefaults := &ServiceDefaults{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, serviceDefaults.GetObjectMeta())
}
