package v1alpha1

import (
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestServiceResolver_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours   ServiceResolver
		Theirs *capi.ServiceResolverConfigEntry
	}{
		"empty fields": {
			Ours: ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceResolverSpec{},
			},
			Theirs: &capi.ServiceResolverConfigEntry{
				Name: "name",
				Kind: capi.ServiceResolver,
			},
		},
		"all fields set": {
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
					},
					ConnectTimeout: 1 * time.Second,
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
									TTL:     1,
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
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.True(t, c.Ours.MatchesConsul(c.Theirs))
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
					},
					ConnectTimeout: 1 * time.Second,
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
									TTL:     1,
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
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := c.Ours.ToConsul()
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

// Test that if status is empty then GetCondition returns nil.
func TestServiceResolver_GetConditionWhenNil(t *testing.T) {
	serviceResolver := &ServiceResolver{}
	require.Nil(t, serviceResolver.GetCondition(ConditionSynced))
}

func TestServiceResolver_Validate(t *testing.T) {
	cases := map[string]struct {
		input          *ServiceResolver
		expectedErrMsg string
	}{
		"valid": {
			input: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceResolverSpec{
					Redirect: &ServiceResolverRedirect{
						Service: "bar",
					},
				},
			},
			expectedErrMsg: "",
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
			expectedErrMsg: "serviceresolver.consul.hashicorp.com \"foo\" is invalid: [spec.failover[failA]: Invalid value: \"{}\": service, serviceSubset, namespace and datacenters cannot all be empty at once, spec.failover[failB]: Invalid value: \"{}\": service, serviceSubset, namespace and datacenters cannot all be empty at once]",
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
			expectedErrMsg: `serviceresolver.consul.hashicorp.com "foo" is invalid: spec.loadBalancer.hashPolicies[0].field: Invalid value: "invalid": must be one of "header", "cookie", "query_parameter"`,
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
			expectedErrMsg: `serviceresolver.consul.hashicorp.com "foo" is invalid: spec.loadBalancer.hashPolicies[0]: Invalid value: "{\"field\":\"header\",\"sourceIP\":true}": cannot set both field and sourceIP`,
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
								Field: "cookie",
								CookieConfig: &CookieConfig{
									Session: true,
									TTL:     100,
								},
							},
						},
					},
				},
			},
			expectedErrMsg: `serviceresolver.consul.hashicorp.com "foo" is invalid: spec.loadBalancer.hashPolicies[0].cookieConfig: Invalid value: "{\"session\":true,\"ttl\":100}": cannot set both session and ttl`,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate()
			if testCase.expectedErrMsg != "" {
				require.EqualError(t, err, testCase.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
