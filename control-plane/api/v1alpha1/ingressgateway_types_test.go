package v1alpha1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIngressGateway_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    IngressGateway
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{},
			},
			Theirs: &capi.IngressGatewayConfigEntry{
				Kind:      capi.IngressGateway,
				Name:      "name",
				Namespace: "foobar",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: true,
		},
		"all fields set matches": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{
					TLS: GatewayTLSConfig{
						Enabled: true,
					},
					Listeners: []IngressListener{
						{
							Port:     8888,
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name1",
									Hosts:     []string{"host1_1", "host1_2"},
									Namespace: "ns1",
								},
								{
									Name:      "name2",
									Hosts:     []string{"host2_1", "host2_2"},
									Namespace: "ns2",
								},
							},
						},
						{
							Port:     9999,
							Protocol: "http",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			Theirs: &capi.IngressGatewayConfigEntry{
				Kind:      capi.IngressGateway,
				Name:      "name",
				Namespace: "foobar",
				TLS: capi.GatewayTLSConfig{
					Enabled: true,
				},
				Listeners: []capi.IngressListener{
					{
						Port:     8888,
						Protocol: "tcp",
						Services: []capi.IngressService{
							{
								Name:      "name1",
								Hosts:     []string{"host1_1", "host1_2"},
								Namespace: "ns1",
							},
							{
								Name:      "name2",
								Hosts:     []string{"host2_1", "host2_2"},
								Namespace: "ns2",
							},
						},
					},
					{
						Port:     9999,
						Protocol: "http",
						Services: []capi.IngressService{
							{
								Name: "*",
							},
						},
					},
				},
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: true,
		},
		"different types does not match": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        "name",
				Kind:        capi.IngressGateway,
				Namespace:   "foobar",
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: false,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.Matches, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

func TestIngressGateway_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours IngressGateway
		Exp  *capi.IngressGatewayConfigEntry
	}{
		"empty fields": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{},
			},
			Exp: &capi.IngressGatewayConfigEntry{
				Kind: capi.IngressGateway,
				Name: "name",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{
					TLS: GatewayTLSConfig{
						Enabled: true,
					},
					Listeners: []IngressListener{
						{
							Port:     8888,
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name1",
									Hosts:     []string{"host1_1", "host1_2"},
									Namespace: "ns1",
								},
								{
									Name:      "name2",
									Hosts:     []string{"host2_1", "host2_2"},
									Namespace: "ns2",
								},
							},
						},
						{
							Port:     9999,
							Protocol: "http",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			Exp: &capi.IngressGatewayConfigEntry{
				Kind: capi.IngressGateway,
				Name: "name",
				TLS: capi.GatewayTLSConfig{
					Enabled: true,
				},
				Listeners: []capi.IngressListener{
					{
						Port:     8888,
						Protocol: "tcp",
						Services: []capi.IngressService{
							{
								Name:      "name1",
								Hosts:     []string{"host1_1", "host1_2"},
								Namespace: "ns1",
							},
							{
								Name:      "name2",
								Hosts:     []string{"host2_1", "host2_2"},
								Namespace: "ns2",
							},
						},
					},
					{
						Port:     9999,
						Protocol: "http",
						Services: []capi.IngressService{
							{
								Name: "*",
							},
						},
					},
				},
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := c.Ours.ToConsul("datacenter")
			ingressGateway, ok := act.(*capi.IngressGatewayConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, ingressGateway)
		})
	}
}

func TestIngressGateway_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *IngressGateway
		namespacesEnabled bool
		expectedErrMsgs   []string
	}{
		"listener.protocol invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "invalid",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].protocol: Invalid value: "invalid": must be one of "tcp", "http", "http2", "grpc"`,
			},
		},
		"len(services) > 0 when protocol==tcp": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name: "svc1",
								},
								{
									Name: "svc2",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services: Invalid value: "[{\"name\":\"svc1\"},{\"name\":\"svc2\"}]": if protocol is "tcp", only a single service is allowed, found 2`,
			},
		},
		"protocol != http when service.name==*": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].name: Invalid value: "*": if name is "*", protocol must be "http" but was "tcp"`,
			},
		},
		"len(hosts) > 0 when service.name==*": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "http",
							Services: []IngressService{
								{
									Name:  "*",
									Hosts: []string{"host1", "host2"},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].hosts: Invalid value: "[\"host1\",\"host2\"]": hosts must be empty if name is "*"`,
			},
		},
		"len(hosts) > 0 when protocol==tcp": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:  "name",
									Hosts: []string{"host1", "host2"},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].hosts: Invalid value: "[\"host1\",\"host2\"]": hosts must be empty if protocol is "tcp"`,
			},
		},
		"service.namespace set when namespaces disabled": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Namespace: "foo",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].namespace: Invalid value: "foo": Consul Enterprise namespaces must be enabled to set service.namespace`,
			},
		},
		"service.namespace set when namespaces enabled": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Namespace: "foo",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
		},
		"multiple errors": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "invalid",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].protocol: Invalid value: "invalid": must be one of "tcp", "http", "http2", "grpc"`,
				`spec.listeners[0].services[0].name: Invalid value: "*": if name is "*", protocol must be "http" but was "invalid"`,
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{NamespacesEnabled: testCase.namespacesEnabled})
			if len(testCase.expectedErrMsgs) != 0 {
				require.Error(t, err)
				for _, s := range testCase.expectedErrMsgs {
					require.Contains(t, err.Error(), s)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
func TestIngressGateway_DefaultNamespaceFields(t *testing.T) {
	namespaceConfig := map[string]struct {
		consulMeta          common.ConsulMeta
		expectedDestination string
	}{
		"disabled": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    false,
				DestinationNamespace: "",
				Mirroring:            false,
				Prefix:               "",
			},
			expectedDestination: "",
		},
		"destinationNS": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "foo",
				Mirroring:            false,
				Prefix:               "",
			},
			expectedDestination: "foo",
		},
		"mirroringEnabledWithoutPrefix": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "",
				Mirroring:            true,
				Prefix:               "",
			},
			expectedDestination: "bar",
		},
		"mirroringWithPrefix": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "",
				Mirroring:            true,
				Prefix:               "ns-",
			},
			expectedDestination: "ns-bar",
		},
	}

	for name, s := range namespaceConfig {
		t.Run(name, func(t *testing.T) {
			input := &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name: "name",
								},
								{
									Name:      "other-name",
									Namespace: "other",
								},
							},
						},
					},
				},
			}
			output := &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Namespace: s.expectedDestination,
								},
								{
									Name:      "other-name",
									Namespace: "other",
								},
							},
						},
					},
				},
			}
			input.DefaultNamespaceFields(s.consulMeta)
			require.True(t, cmp.Equal(input, output))
		})
	}
}

func TestIngressGateway_AddFinalizer(t *testing.T) {
	ingressGateway := &IngressGateway{}
	ingressGateway.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, ingressGateway.ObjectMeta.Finalizers)
}

func TestIngressGateway_RemoveFinalizer(t *testing.T) {
	ingressGateway := &IngressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	ingressGateway.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, ingressGateway.ObjectMeta.Finalizers)
}

func TestIngressGateway_SetSyncedCondition(t *testing.T) {
	ingressGateway := &IngressGateway{}
	ingressGateway.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, ingressGateway.Status.Conditions[0].Status)
	require.Equal(t, "reason", ingressGateway.Status.Conditions[0].Reason)
	require.Equal(t, "message", ingressGateway.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, ingressGateway.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestIngressGateway_SetLastSyncedTime(t *testing.T) {
	ingressGateway := &IngressGateway{}
	syncedTime := metav1.NewTime(time.Now())
	ingressGateway.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, ingressGateway.Status.LastSyncedTime)
}

func TestIngressGateway_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			ingressGateway := &IngressGateway{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, ingressGateway.SyncedConditionStatus())
		})
	}
}

func TestIngressGateway_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&IngressGateway{}).GetCondition(ConditionSynced))
}

func TestIngressGateway_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&IngressGateway{}).SyncedConditionStatus())
}

func TestIngressGateway_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&IngressGateway{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestIngressGateway_ConsulKind(t *testing.T) {
	require.Equal(t, capi.IngressGateway, (&IngressGateway{}).ConsulKind())
}

func TestIngressGateway_KubeKind(t *testing.T) {
	require.Equal(t, "ingressgateway", (&IngressGateway{}).KubeKind())
}

func TestIngressGateway_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&IngressGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestIngressGateway_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&IngressGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestIngressGateway_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&IngressGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestIngressGateway_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&IngressGateway{}).ConsulGlobalResource())
}

func TestIngressGateway_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	ingressGateway := &IngressGateway{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, ingressGateway.GetObjectMeta())
}
