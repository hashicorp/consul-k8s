package v1alpha1

import (
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestServiceResolver_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    ServiceResolver
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceResolverSpec{},
			},
			Theirs: &capi.ServiceResolverConfigEntry{
				Name:        "name",
				Kind:        capi.ServiceResolver,
				Namespace:   "foobar",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
			Matches: true,
		},
		"all fields set matches": {
			Ours: ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceResolverSpec{
					DefaultSubset: "default_subset",
					Subsets: map[string]ServiceResolverSubset{
						"subset1": {
							Filter:      "filter1",
							OnlyPassing: true,
						},
						"subset2": {
							Filter:      "filter2",
							OnlyPassing: false,
						},
					},
					Redirect: &ServiceResolverRedirect{
						Service:       "redirect",
						ServiceSubset: "redirect_subset",
						Namespace:     "redirect_namespace",
						Datacenter:    "redirect_datacenter",
						Peer:          "redirect_peer",
					},
					Failover: map[string]ServiceResolverFailover{
						"failover1": {
							Service:       "failover1",
							ServiceSubset: "failover_subset1",
							Namespace:     "failover_namespace1",
							Datacenters:   []string{"failover1_dc1", "failover1_dc2"},
						},
						"failover2": {
							Service:       "failover2",
							ServiceSubset: "failover_subset2",
							Namespace:     "failover_namespace2",
							Datacenters:   []string{"failover2_dc1", "failover2_dc2"},
						},
						"failover3": {
							Targets: []ServiceResolverFailoverTarget{
								{Peer: "failover_peer3"},
								{Partition: "failover_partition3", Namespace: "failover_namespace3"},
							},
						},
					},
					ConnectTimeout: metav1.Duration{Duration: 1 * time.Second},
					LoadBalancer: &LoadBalancer{
						Policy: "policy",
						RingHashConfig: &RingHashConfig{
							MinimumRingSize: 1,
							MaximumRingSize: 2,
						},
						LeastRequestConfig: &LeastRequestConfig{
							ChoiceCount: 1,
						},
						HashPolicies: []HashPolicy{
							{
								Field:      "field",
								FieldValue: "value",
								CookieConfig: &CookieConfig{
									Session: true,
									TTL:     metav1.Duration{Duration: 1},
									Path:    "path",
								},
								SourceIP: true,
								Terminal: true,
							},
						},
					},
				},
			},
			Theirs: &capi.ServiceResolverConfigEntry{
				Name:          "name",
				Kind:          capi.ServiceResolver,
				DefaultSubset: "default_subset",
				Subsets: map[string]capi.ServiceResolverSubset{
					"subset1": {
						Filter:      "filter1",
						OnlyPassing: true,
					},
					"subset2": {
						Filter:      "filter2",
						OnlyPassing: false,
					},
				},
				Redirect: &capi.ServiceResolverRedirect{
					Service:       "redirect",
					ServiceSubset: "redirect_subset",
					Namespace:     "redirect_namespace",
					Datacenter:    "redirect_datacenter",
					Peer:          "redirect_peer",
				},
				Failover: map[string]capi.ServiceResolverFailover{
					"failover1": {
						Service:       "failover1",
						ServiceSubset: "failover_subset1",
						Namespace:     "failover_namespace1",
						Datacenters:   []string{"failover1_dc1", "failover1_dc2"},
					},
					"failover2": {
						Service:       "failover2",
						ServiceSubset: "failover_subset2",
						Namespace:     "failover_namespace2",
						Datacenters:   []string{"failover2_dc1", "failover2_dc2"},
					},
					"failover3": {
						Targets: []capi.ServiceResolverFailoverTarget{
							{Peer: "failover_peer3"},
							{Partition: "failover_partition3", Namespace: "failover_namespace3"},
						},
					},
				},
				ConnectTimeout: 1 * time.Second,
				LoadBalancer: &capi.LoadBalancer{
					Policy: "policy",
					RingHashConfig: &capi.RingHashConfig{
						MinimumRingSize: 1,
						MaximumRingSize: 2,
					},
					LeastRequestConfig: &capi.LeastRequestConfig{
						ChoiceCount: 1,
					},
					HashPolicies: []capi.HashPolicy{
						{
							Field:      "field",
							FieldValue: "value",
							CookieConfig: &capi.CookieConfig{
								Session: true,
								TTL:     1,
								Path:    "path",
							},
							SourceIP: true,
							Terminal: true,
						},
					},
				},
			},
			Matches: true,
		},
		"different types does not match": {
			Ours: ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceResolverSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        "name",
				Kind:        capi.ServiceResolver,
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

func TestServiceResolver_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ServiceResolver
		Exp  *capi.ServiceResolverConfigEntry
	}{
		"empty fields": {
			Ours: ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceResolverSpec{},
			},
			Exp: &capi.ServiceResolverConfigEntry{
				Name: "name",
				Kind: capi.ServiceResolver,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceResolverSpec{
					DefaultSubset: "default_subset",
					Subsets: map[string]ServiceResolverSubset{
						"subset1": {
							Filter:      "filter1",
							OnlyPassing: true,
						},
						"subset2": {
							Filter:      "filter2",
							OnlyPassing: false,
						},
					},
					Redirect: &ServiceResolverRedirect{
						Service:       "redirect",
						ServiceSubset: "redirect_subset",
						Namespace:     "redirect_namespace",
						Datacenter:    "redirect_datacenter",
						Partition:     "redirect_partition",
					},
					Failover: map[string]ServiceResolverFailover{
						"failover1": {
							Service:       "failover1",
							ServiceSubset: "failover_subset1",
							Namespace:     "failover_namespace1",
							Datacenters:   []string{"failover1_dc1", "failover1_dc2"},
						},
						"failover2": {
							Service:       "failover2",
							ServiceSubset: "failover_subset2",
							Namespace:     "failover_namespace2",
							Datacenters:   []string{"failover2_dc1", "failover2_dc2"},
						},
						"failover3": {
							Targets: []ServiceResolverFailoverTarget{
								{Peer: "failover_peer3"},
								{Partition: "failover_partition3", Namespace: "failover_namespace3"},
							},
						},
					},
					ConnectTimeout: metav1.Duration{Duration: 1 * time.Second},
					LoadBalancer: &LoadBalancer{
						Policy: "policy",
						RingHashConfig: &RingHashConfig{
							MinimumRingSize: 1,
							MaximumRingSize: 2,
						},
						LeastRequestConfig: &LeastRequestConfig{
							ChoiceCount: 1,
						},
						HashPolicies: []HashPolicy{
							{
								Field:      "field",
								FieldValue: "value",
								CookieConfig: &CookieConfig{
									Session: true,
									TTL:     metav1.Duration{Duration: 1},
									Path:    "path",
								},
								SourceIP: true,
								Terminal: true,
							},
						},
					},
				},
			},
			Exp: &capi.ServiceResolverConfigEntry{
				Name:          "name",
				Kind:          capi.ServiceResolver,
				DefaultSubset: "default_subset",
				Subsets: map[string]capi.ServiceResolverSubset{
					"subset1": {
						Filter:      "filter1",
						OnlyPassing: true,
					},
					"subset2": {
						Filter:      "filter2",
						OnlyPassing: false,
					},
				},
				Redirect: &capi.ServiceResolverRedirect{
					Service:       "redirect",
					ServiceSubset: "redirect_subset",
					Namespace:     "redirect_namespace",
					Datacenter:    "redirect_datacenter",
					Partition:     "redirect_partition",
				},
				Failover: map[string]capi.ServiceResolverFailover{
					"failover1": {
						Service:       "failover1",
						ServiceSubset: "failover_subset1",
						Namespace:     "failover_namespace1",
						Datacenters:   []string{"failover1_dc1", "failover1_dc2"},
					},
					"failover2": {
						Service:       "failover2",
						ServiceSubset: "failover_subset2",
						Namespace:     "failover_namespace2",
						Datacenters:   []string{"failover2_dc1", "failover2_dc2"},
					},
					"failover3": {
						Targets: []capi.ServiceResolverFailoverTarget{
							{Peer: "failover_peer3"},
							{Partition: "failover_partition3", Namespace: "failover_namespace3"},
						},
					},
				},
				ConnectTimeout: 1 * time.Second,
				LoadBalancer: &capi.LoadBalancer{
					Policy: "policy",
					RingHashConfig: &capi.RingHashConfig{
						MinimumRingSize: 1,
						MaximumRingSize: 2,
					},
					LeastRequestConfig: &capi.LeastRequestConfig{
						ChoiceCount: 1,
					},
					HashPolicies: []capi.HashPolicy{
						{
							Field:      "field",
							FieldValue: "value",
							CookieConfig: &capi.CookieConfig{
								Session: true,
								TTL:     1,
								Path:    "path",
							},
							SourceIP: true,
							Terminal: true,
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
			serviceResolver, ok := act.(*capi.ServiceResolverConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, serviceResolver)
		})
	}
}

func TestServiceResolver_AddFinalizer(t *testing.T) {
	serviceResolver := &ServiceResolver{}
	serviceResolver.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, serviceResolver.ObjectMeta.Finalizers)
}

func TestServiceResolver_RemoveFinalizer(t *testing.T) {
	serviceResolver := &ServiceResolver{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	serviceResolver.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, serviceResolver.ObjectMeta.Finalizers)
}

func TestServiceResolver_SetSyncedCondition(t *testing.T) {
	serviceResolver := &ServiceResolver{}
	serviceResolver.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, serviceResolver.Status.Conditions[0].Status)
	require.Equal(t, "reason", serviceResolver.Status.Conditions[0].Reason)
	require.Equal(t, "message", serviceResolver.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, serviceResolver.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceResolver_SetLastSyncedTime(t *testing.T) {
	serviceResolver := &ServiceResolver{}
	syncedTime := metav1.NewTime(time.Now())
	serviceResolver.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, serviceResolver.Status.LastSyncedTime)
}

func TestServiceResolver_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			serviceResolver := &ServiceResolver{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, serviceResolver.SyncedConditionStatus())
		})
	}
}

func TestServiceResolver_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ServiceResolver{}).GetCondition(ConditionSynced))
}

func TestServiceResolver_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ServiceResolver{}).SyncedConditionStatus())
}

func TestServiceResolver_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ServiceResolver{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestServiceResolver_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ServiceResolver, (&ServiceResolver{}).ConsulKind())
}

func TestServiceResolver_KubeKind(t *testing.T) {
	require.Equal(t, "serviceresolver", (&ServiceResolver{}).KubeKind())
}

func TestServiceResolver_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceResolver{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceResolver_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceResolver{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestServiceResolver_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&ServiceResolver{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestServiceResolver_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&ServiceResolver{}).ConsulGlobalResource())
}

func TestServiceResolver_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	serviceResolver := &ServiceResolver{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, serviceResolver.GetObjectMeta())
}

func TestServiceResolver_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *ServiceResolver
		namespacesEnabled bool
		partitionsEnabled bool
		expectedErrMsgs   []string
	}{
		"namespaces enabled: valid": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Redirect: &ServiceResolverRedirect{
						Service:   "bar",
						Namespace: "namespace-a",
					},
					Failover: map[string]ServiceResolverFailover{
						"failA": {
							Service:   "baz",
							Namespace: "namespace-b",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: false,
			expectedErrMsgs:   nil,
		},
		"namespaces disabled: valid": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Redirect: &ServiceResolverRedirect{
						Service: "bar",
					},
					Failover: map[string]ServiceResolverFailover{
						"failA": {
							Service: "baz",
						},
					},
				},
			},
			namespacesEnabled: false,
			partitionsEnabled: false,
			expectedErrMsgs:   nil,
		},
		"partitions enabled: valid": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Redirect: &ServiceResolverRedirect{
						Service:   "bar",
						Namespace: "namespace-a",
						Partition: "other",
					},
					Failover: map[string]ServiceResolverFailover{
						"failA": {
							Service:   "baz",
							Namespace: "namespace-b",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: true,
			expectedErrMsgs:   nil,
		},
		"partitions disabled: valid": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Redirect: &ServiceResolverRedirect{
						Service: "bar",
					},
					Failover: map[string]ServiceResolverFailover{
						"failA": {
							Service: "baz",
						},
					},
				},
			},
			namespacesEnabled: false,
			partitionsEnabled: false,
			expectedErrMsgs:   nil,
		},
		"failover service, servicesubset, namespace, datacenters empty": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Failover: map[string]ServiceResolverFailover{
						"failA": {
							Service:       "",
							ServiceSubset: "",
							Namespace:     "",
							Datacenters:   nil,
						},
						"failB": {
							Service:       "",
							ServiceSubset: "",
							Namespace:     "",
							Datacenters:   nil,
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				"spec.failover[failA]: Invalid value: \"{}\": service, serviceSubset, namespace, datacenters, and targets cannot all be empty at once",
				"spec.failover[failB]: Invalid value: \"{}\": service, serviceSubset, namespace, datacenters, and targets cannot all be empty at once",
			},
		},
		"hashPolicy.field invalid": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					LoadBalancer: &LoadBalancer{
						HashPolicies: []HashPolicy{
							{
								Field: "invalid",
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`serviceresolver.consul.hashicorp.com "foo" is invalid: [spec.loadBalancer.hashPolicies[0].field: Invalid value: "invalid": must be one of "header", "cookie", "query_parameter"`,
				`spec.loadBalancer.hashPolicies[0].fieldValue: Invalid value: "": fieldValue cannot be empty if field is set`,
			},
		},
		"hashPolicy.field without fieldValue": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					LoadBalancer: &LoadBalancer{
						HashPolicies: []HashPolicy{
							{
								Field: "header",
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`serviceresolver.consul.hashicorp.com "foo" is invalid: spec.loadBalancer.hashPolicies[0].fieldValue: Invalid value: "": fieldValue cannot be empty if field is set`,
			},
		},
		"hashPolicy just sourceIP set": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					LoadBalancer: &LoadBalancer{
						HashPolicies: []HashPolicy{
							{
								SourceIP: true,
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs:   nil,
		},
		"hashPolicy sourceIP and field set": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					LoadBalancer: &LoadBalancer{
						HashPolicies: []HashPolicy{
							{
								Field:    "header",
								SourceIP: true,
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`serviceresolver.consul.hashicorp.com "foo" is invalid: spec.loadBalancer.hashPolicies[0]: Invalid value: "{\"field\":\"header\",\"sourceIP\":true}": cannot set both field and sourceIP`,
			},
		},
		"hashPolicy nothing set is valid": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					LoadBalancer: &LoadBalancer{
						HashPolicies: []HashPolicy{
							{},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs:   nil,
		},
		"cookieConfig session and ttl set": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					LoadBalancer: &LoadBalancer{
						HashPolicies: []HashPolicy{
							{
								Field:      "cookie",
								FieldValue: "cookiename",
								CookieConfig: &CookieConfig{
									Session: true,
									TTL:     metav1.Duration{Duration: 100},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`serviceresolver.consul.hashicorp.com "foo" is invalid: spec.loadBalancer.hashPolicies[0].cookieConfig: Invalid value: "{\"session\":true,\"ttl\":\"100ns\"}": cannot set both session and ttl`,
			},
		},
		"namespaces disabled: redirect namespace specified": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Redirect: &ServiceResolverRedirect{
						Namespace: "namespace-a",
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				"serviceresolver.consul.hashicorp.com \"foo\" is invalid: spec.redirect.namespace: Invalid value: \"namespace-a\": Consul Enterprise namespaces must be enabled to set redirect.namespace",
			},
		},
		"partitions disabled: redirect partition specified": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Redirect: &ServiceResolverRedirect{
						Namespace: "namespace-a",
						Partition: "other",
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: false,
			expectedErrMsgs: []string{
				"serviceresolver.consul.hashicorp.com \"foo\" is invalid: spec.redirect.partition: Invalid value: \"other\": Consul Enterprise partitions must be enabled to set redirect.partition",
			},
		},
		"namespaces disabled: single failover namespace specified": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Failover: map[string]ServiceResolverFailover{
						"failA": {
							Namespace: "namespace-a",
						},
					},
				},
			},
			expectedErrMsgs: []string{
				"serviceresolver.consul.hashicorp.com \"foo\" is invalid: spec.failover[failA].namespace: Invalid value: \"namespace-a\": Consul Enterprise namespaces must be enabled to set failover.namespace",
			},
			namespacesEnabled: false,
		},
		"namespaces disabled: multiple failover namespaces specified": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Failover: map[string]ServiceResolverFailover{
						"failA": {
							Namespace: "namespace-a",
						},
						"failB": {
							Namespace: "namespace-b",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				"spec.failover[failA].namespace: Invalid value: \"namespace-a\": Consul Enterprise namespaces must be enabled to set failover.namespace",
				"spec.failover[failB].namespace: Invalid value: \"namespace-b\": Consul Enterprise namespaces must be enabled to set failover.namespace",
			},
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{NamespacesEnabled: testCase.namespacesEnabled, PartitionsEnabled: testCase.partitionsEnabled})
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
