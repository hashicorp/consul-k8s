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

func TestServiceIntentions_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    ServiceIntentions
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceIntentionsSpec{},
			},
			Theirs: &capi.ServiceIntentionsConfigEntry{
				Name:        "",
				Kind:        capi.ServiceIntentions,
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
			Ours: ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "svc-name",
						Namespace: "test",
					},
					Sources: []*SourceIntention{
						{
							Name:        "svc1",
							Namespace:   "test",
							Partition:   "test",
							Action:      "allow",
							Description: "allow access from svc1",
						},
						{
							Name:        "*",
							Namespace:   "not-test",
							Partition:   "not-test",
							Action:      "deny",
							Description: "disallow access from namespace not-test",
						},
						{
							Name:      "svc-2",
							Namespace: "bar",
							Partition: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathExact:  "/foo",
										PathPrefix: "/bar",
										PathRegex:  "/baz",
										Header: IntentionHTTPHeaderPermissions{
											{
												Name:    "header",
												Present: true,
												Exact:   "exact",
												Prefix:  "prefix",
												Suffix:  "suffix",
												Regex:   "regex",
												Invert:  true,
											},
										},
										Methods: []string{
											"GET",
											"PUT",
										},
									},
								},
							},
							Description: "an L7 config",
						},
					},
				},
			},
			Theirs: &capi.ServiceIntentionsConfigEntry{
				Kind:      capi.ServiceIntentions,
				Name:      "svc-name",
				Namespace: "test",
				Sources: []*capi.SourceIntention{
					{
						Name:        "svc1",
						Namespace:   "test",
						Partition:   "test",
						Action:      "allow",
						Precedence:  0,
						Description: "allow access from svc1",
					},
					{
						Name:        "*",
						Namespace:   "not-test",
						Partition:   "not-test",
						Action:      "deny",
						Precedence:  1,
						Description: "disallow access from namespace not-test",
					},
					{
						Name:      "svc-2",
						Namespace: "bar",
						Partition: "bar",
						Permissions: []*capi.IntentionPermission{
							{
								Action: "allow",
								HTTP: &capi.IntentionHTTPPermission{
									PathExact:  "/foo",
									PathPrefix: "/bar",
									PathRegex:  "/baz",
									Header: []capi.IntentionHTTPHeaderPermission{
										{
											Name:    "header",
											Present: true,
											Exact:   "exact",
											Prefix:  "prefix",
											Suffix:  "suffix",
											Regex:   "regex",
											Invert:  true,
										},
									},
									Methods: []string{
										"GET",
										"PUT",
									},
								},
							},
						},
						Description: "an L7 config",
					},
				},
				Meta: nil,
			},
			Matches: true,
		},
		"different types does not match": {
			Ours: ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceIntentionsSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        "name",
				Kind:        capi.ServiceIntentions,
				Namespace:   "foobar",
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: false,
		},
		"different order of sources matches": {
			Ours: ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:   "*",
							Action: "allow",
						},
						{
							Name:   "foo",
							Action: "allow",
						},
					},
				},
			},
			Theirs: &capi.ServiceIntentionsConfigEntry{
				Name:        "bar",
				Kind:        capi.ServiceIntentions,
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				Sources: []*capi.SourceIntention{
					{
						Name:   "foo",
						Action: "allow",
					},
					{
						Name:   "*",
						Action: "allow",
					},
				},
			},
			Matches: true,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.Matches, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

func TestServiceIntentions_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ServiceIntentions
		Exp  *capi.ServiceIntentionsConfigEntry
	}{
		"empty fields": {
			Ours: ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceIntentionsSpec{},
			},
			Exp: &capi.ServiceIntentionsConfigEntry{
				Name: "",
				Kind: capi.ServiceIntentions,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "svc-name",
						Namespace: "dest-ns",
					},
					Sources: []*SourceIntention{
						{
							Name:        "svc1",
							Namespace:   "test",
							Partition:   "test",
							Action:      "allow",
							Description: "allow access from svc1",
						},
						{
							Name:        "*",
							Namespace:   "not-test",
							Partition:   "not-test",
							Action:      "deny",
							Description: "disallow access from namespace not-test",
						},
						{
							Name:      "svc-2",
							Namespace: "bar",
							Partition: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathExact:  "/foo",
										PathPrefix: "/bar",
										PathRegex:  "/baz",
										Header: IntentionHTTPHeaderPermissions{
											{
												Name:    "header",
												Present: true,
												Exact:   "exact",
												Prefix:  "prefix",
												Suffix:  "suffix",
												Regex:   "regex",
												Invert:  true,
											},
										},
										Methods: []string{
											"GET",
											"PUT",
										},
									},
								},
							},
							Description: "an L7 config",
						},
					},
				},
			},
			Exp: &capi.ServiceIntentionsConfigEntry{
				Kind:      capi.ServiceIntentions,
				Name:      "svc-name",
				Namespace: "dest-ns",
				Sources: []*capi.SourceIntention{
					{
						Name:        "svc1",
						Namespace:   "test",
						Partition:   "test",
						Action:      "allow",
						Description: "allow access from svc1",
					},
					{
						Name:        "*",
						Namespace:   "not-test",
						Partition:   "not-test",
						Action:      "deny",
						Description: "disallow access from namespace not-test",
					},
					{
						Name:      "svc-2",
						Namespace: "bar",
						Partition: "bar",
						Permissions: []*capi.IntentionPermission{
							{
								Action: "allow",
								HTTP: &capi.IntentionHTTPPermission{
									PathExact:  "/foo",
									PathPrefix: "/bar",
									PathRegex:  "/baz",
									Header: []capi.IntentionHTTPHeaderPermission{
										{
											Name:    "header",
											Present: true,
											Exact:   "exact",
											Prefix:  "prefix",
											Suffix:  "suffix",
											Regex:   "regex",
											Invert:  true,
										},
									},
									Methods: []string{
										"GET",
										"PUT",
									},
								},
							},
						},
						Description: "an L7 config",
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
			serviceIntentions, ok := act.(*capi.ServiceIntentionsConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, serviceIntentions)
		})
	}
}

func TestServiceIntentions_AddFinalizer(t *testing.T) {
	serviceIntentions := &ServiceIntentions{}
	serviceIntentions.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, serviceIntentions.ObjectMeta.Finalizers)
}

func TestServiceIntentions_RemoveFinalizer(t *testing.T) {
	serviceIntentions := &ServiceIntentions{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	serviceIntentions.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, serviceIntentions.ObjectMeta.Finalizers)
}

func TestServiceIntentions_SetSyncedCondition(t *testing.T) {
	serviceIntentions := &ServiceIntentions{}
	serviceIntentions.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, serviceIntentions.Status.Conditions[0].Status)
	require.Equal(t, "reason", serviceIntentions.Status.Conditions[0].Reason)
	require.Equal(t, "message", serviceIntentions.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, serviceIntentions.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceIntentions_SetLastSyncedTime(t *testing.T) {
	serviceIntentions := &ServiceIntentions{}
	syncedTime := metav1.NewTime(time.Now())
	serviceIntentions.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, serviceIntentions.Status.LastSyncedTime)
}

func TestServiceIntentions_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			serviceIntentions := &ServiceIntentions{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, serviceIntentions.SyncedConditionStatus())
		})
	}
}

func TestServiceIntentions_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ServiceIntentions{}).GetCondition(ConditionSynced))
}

func TestServiceIntentions_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ServiceIntentions{}).SyncedConditionStatus())
}

func TestServiceIntentions_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ServiceIntentions{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestServiceIntentions_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ServiceIntentions, (&ServiceIntentions{}).ConsulKind())
}

func TestServiceIntentions_KubeKind(t *testing.T) {
	require.Equal(t, "serviceintentions", (&ServiceIntentions{}).KubeKind())
}

func TestServiceIntentions_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceIntentions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: ServiceIntentionsSpec{
			Destination: Destination{
				Name:      "foo",
				Namespace: "baz",
			},
		},
	}).ConsulName())
}

func TestServiceIntentions_KubernetesName(t *testing.T) {
	require.Equal(t, "test", (&ServiceIntentions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: ServiceIntentionsSpec{
			Destination: Destination{
				Name:      "foo",
				Namespace: "baz",
			},
		},
	}).KubernetesName())
}

func TestServiceIntentions_ConsulNamespace(t *testing.T) {
	require.Equal(t, "baz", (&ServiceIntentions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: ServiceIntentionsSpec{
			Destination: Destination{
				Name:      "foo",
				Namespace: "baz",
			},
		},
	}).ConsulMirroringNS())
}

func TestServiceIntentions_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&ServiceIntentions{}).ConsulGlobalResource())
}

func TestServiceIntentions_ConsulNamespaceWithWildcard(t *testing.T) {
	require.Equal(t, common.WildcardNamespace, (&ServiceIntentions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: ServiceIntentionsSpec{
			Destination: Destination{
				Name:      "foo",
				Namespace: "*",
			},
		},
	}).ConsulMirroringNS())
}

func TestServiceIntentions_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	serviceIntentions := &ServiceIntentions{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, serviceIntentions.GetObjectMeta())
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
func TestServiceIntentions_DefaultNamespaceFields(t *testing.T) {
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
			input := &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "bar",
					},
				},
			}
			output := &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: s.expectedDestination,
					},
				},
			}
			input.DefaultNamespaceFields(s.consulMeta)
			require.True(t, cmp.Equal(input, output))
		})
	}
}

func TestServiceIntentions_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *ServiceIntentions
		namespacesEnabled bool
		partitionsEnabled bool
		expectedErrMsgs   []string
	}{
		"partitions enabled: valid": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Namespace: "web",
							Partition: "web",
							Action:    "allow",
						},
						{
							Name:      "db",
							Namespace: "db",
							Partition: "db",
							Action:    "deny",
						},
						{
							Name:      "bar",
							Namespace: "bar",
							Partition: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathExact: "/foo",
										Header: IntentionHTTPHeaderPermissions{
											{
												Name:    "header",
												Present: true,
												Invert:  true,
											},
										},
										Methods: []string{
											"GET",
											"PUT",
										},
									},
								},
							},
							Description: "an L7 config",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: true,
			expectedErrMsgs:   nil,
		},
		"namespaces enabled: valid": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Namespace: "web",
							Action:    "allow",
						},
						{
							Name:      "db",
							Namespace: "db",
							Action:    "deny",
						},
						{
							Name:      "bar",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathExact: "/foo",
										Header: IntentionHTTPHeaderPermissions{
											{
												Name:    "header",
												Present: true,
												Invert:  true,
											},
										},
										Methods: []string{
											"GET",
											"PUT",
										},
									},
								},
							},
							Description: "an L7 config",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: false,
			expectedErrMsgs:   nil,
		},
		"namespaces disabled: valid": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "dest-service",
					},
					Sources: SourceIntentions{
						{
							Name:   "web",
							Action: "allow",
						},
						{
							Name:   "db",
							Action: "deny",
						},
						{
							Name: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathRegex: "/baz",
										Header: IntentionHTTPHeaderPermissions{
											{
												Name:   "header",
												Regex:  "regex",
												Invert: true,
											},
										},
										Methods: []string{
											"GET",
											"PUT",
										},
									},
								},
							},
							Description: "an L7 config",
						},
					},
				},
			},
			namespacesEnabled: false,
			partitionsEnabled: false,
			expectedErrMsgs:   nil,
		},
		"no sources": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources: Required value: at least one source must be specified`,
			},
		},
		"invalid action": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Namespace: "web",
							Action:    "foo",
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].action: Invalid value: "foo": must be one of "allow", "deny"`,
			},
		},
		"invalid permissions.http.pathPrefix": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathPrefix: "bar",
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0].pathPrefix: Invalid value: "bar": must begin with a '/'`,
			},
		},
		"invalid permissions.http pathPrefix,pathExact specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathPrefix: "/bar",
										PathExact:  "/foo",
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0]: Invalid value: "{\"pathExact\":\"/foo\",\"pathPrefix\":\"/bar\"}": at most only one of pathExact, pathPrefix, or pathRegex may be configured.`,
			},
		},
		"invalid permissions.http pathPrefix,pathRegex specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathPrefix: "/bar",
										PathRegex:  "foo",
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0]: Invalid value: "{\"pathPrefix\":\"/bar\",\"pathRegex\":\"foo\"}": at most only one of pathExact, pathPrefix, or pathRegex may be configured.`,
			},
		},
		"invalid permissions.http pathRegex,pathExact specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathRegex: "bar",
										PathExact: "/foo",
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0]: Invalid value: "{\"pathExact\":\"/foo\",\"pathRegex\":\"bar\"}": at most only one of pathExact, pathPrefix, or pathRegex may be configured.`,
			},
		},
		"invalid permissions.http.pathExact": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathExact: "bar",
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0].pathExact: Invalid value: "bar": must begin with a '/'`,
			},
		},
		"invalid permissions.http.methods": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										Methods: []string{
											"FOO",
											"GET",
											"BAR",
											"GET",
											"POST",
										},
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: [spec.sources[0].permissions[0].methods[0]: Invalid value: "FOO": must be one of "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE", spec.sources[0].permissions[0].methods[2]: Invalid value: "BAR": must be one of "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE", spec.sources[0].permissions[0].methods[3]: Invalid value: "GET": method listed more than once.`,
			},
		},
		"invalid permissions.http.header": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										Header: IntentionHTTPHeaderPermissions{
											{
												Name:    "exact-present",
												Present: true,
												Exact:   "foobar",
											},
											{
												Name:   "prefix-exact",
												Exact:  "foobar",
												Prefix: "barfood",
											},
											{
												Name:   "suffix-prefix",
												Prefix: "foo",
												Suffix: "bar",
											},
											{
												Name:   "suffix-regex",
												Suffix: "bar",
												Regex:  "foo",
											},
											{
												Name:    "regex-present",
												Present: true,
												Regex:   "foobar",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`spec.sources[0].permissions[0].header[0]: Invalid value: "{\"name\":\"exact-present\",\"present\":true,\"exact\":\"foobar\"}": at most only one of exact, prefix, suffix, regex, or present may be configured.`,
				`spec.sources[0].permissions[0].header[1]: Invalid value: "{\"name\":\"prefix-exact\",\"exact\":\"foobar\",\"prefix\":\"barfood\"}": at most only one of exact, prefix, suffix, regex, or present may be configured.`,
				`spec.sources[0].permissions[0].header[2]: Invalid value: "{\"name\":\"suffix-prefix\",\"prefix\":\"foo\",\"suffix\":\"bar\"}": at most only one of exact, prefix, suffix, regex, or present may be configured.`,
				`spec.sources[0].permissions[0].header[3]: Invalid value: "{\"name\":\"suffix-regex\",\"suffix\":\"bar\",\"regex\":\"foo\"}": at most only one of exact, prefix, suffix, regex, or present may be configured.`,
				`spec.sources[0].permissions[0].header[4]: Invalid value: "{\"name\":\"regex-present\",\"present\":true,\"regex\":\"foobar\"}": at most only one of exact, prefix, suffix, regex, or present may be configured.`,
			},
		},
		"invalid permissions.action": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Permissions: IntentionPermissions{
								{
									Action: "foobar",
									HTTP: &IntentionHTTPPermission{
										PathExact: "/bar",
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0].action: Invalid value: "foobar": must be one of "allow", "deny"`,
			},
		},
		"both action and permissions specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace",
					},
					Sources: SourceIntentions{
						{
							Name:      "svc-2",
							Namespace: "bar",
							Action:    "deny",
							Permissions: IntentionPermissions{
								{
									Action: "allow",
									HTTP: &IntentionHTTPPermission{
										PathExact: "/bar",
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0]: Invalid value: "{\"name\":\"svc-2\",\"namespace\":\"bar\",\"action\":\"deny\",\"permissions\":[{\"action\":\"allow\",\"http\":{\"pathExact\":\"/bar\"}}]}": action and permissions are mutually exclusive and only one of them can be specified`,
			},
		},
		"namespaces disabled: destination namespace specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace-a",
					},
					Sources: SourceIntentions{
						{
							Name:   "web",
							Action: "allow",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.destination.namespace: Invalid value: "namespace-a": Consul Enterprise namespaces must be enabled to set destination.namespace`,
			},
		},
		"namespaces disabled: single source namespace specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "dest-service",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "namespace-a",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].namespace: Invalid value: "namespace-a": Consul Enterprise namespaces must be enabled to set source.namespace`,
			},
		},
		"namespaces disabled: multiple source namespaces specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "dest-service",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "namespace-a",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "namespace-b",
						},
						{
							Name:      "bar",
							Namespace: "namespace-c",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.sources[0].namespace: Invalid value: "namespace-a": Consul Enterprise namespaces must be enabled to set source.namespace`,
				`spec.sources[1].namespace: Invalid value: "namespace-b": Consul Enterprise namespaces must be enabled to set source.namespace`,
				`spec.sources[2].namespace: Invalid value: "namespace-c": Consul Enterprise namespaces must be enabled to set source.namespace`,
			},
		},
		"namespaces disabled: destination and multiple source namespaces specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace-a",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "namespace-b",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "namespace-c",
						},
						{
							Name:      "bar",
							Namespace: "namespace-d",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.destination.namespace: Invalid value: "namespace-a": Consul Enterprise namespaces must be enabled to set destination.namespace`,
				`spec.sources[0].namespace: Invalid value: "namespace-b": Consul Enterprise namespaces must be enabled to set source.namespace`,
				`spec.sources[1].namespace: Invalid value: "namespace-c": Consul Enterprise namespaces must be enabled to set source.namespace`,
				`spec.sources[2].namespace: Invalid value: "namespace-d": Consul Enterprise namespaces must be enabled to set source.namespace`,
			},
		},
		"partitions disabled: single source partition specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace-a",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "namespace-b",
							Partition: "partition-other",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "namespace-c",
						},
						{
							Name:      "bar",
							Namespace: "namespace-d",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: false,
			expectedErrMsgs: []string{
				`spec.sources[0].partition: Invalid value: "partition-other": Consul Enterprise Admin Partitions must be enabled to set source.partition`,
			},
		},
		"partitions disabled: multiple source partition specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace-a",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "namespace-b",
							Partition: "partition-other",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "namespace-c",
							Partition: "partition-first",
						},
						{
							Name:      "bar",
							Namespace: "namespace-d",
							Partition: "partition-foo",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: false,
			expectedErrMsgs: []string{
				`spec.sources[0].partition: Invalid value: "partition-other": Consul Enterprise Admin Partitions must be enabled to set source.partition`,
				`spec.sources[1].partition: Invalid value: "partition-first": Consul Enterprise Admin Partitions must be enabled to set source.partition`,
				`spec.sources[2].partition: Invalid value: "partition-foo": Consul Enterprise Admin Partitions must be enabled to set source.partition`,
			},
		},
		"single source peer and partition specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace-a",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "namespace-b",
							Partition: "partition-other",
							Peer:      "peer-other",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "namespace-c",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`spec.sources[0]: Invalid value: v1alpha1.SourceIntention{Name:"web", Namespace:"namespace-b", Peer:"peer-other", Partition:"partition-other", Action:"allow", Permissions:v1alpha1.IntentionPermissions(nil), Description:""}: Both source.peer and source.partition cannot be set.`,
			},
		},
		"multiple source peer and partition specified": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "namespace-a",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "namespace-b",
							Partition: "partition-other",
							Peer:      "peer-other",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "namespace-c",
							Partition: "partition-2",
							Peer:      "peer-2",
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`spec.sources[0]: Invalid value: v1alpha1.SourceIntention{Name:"web", Namespace:"namespace-b", Peer:"peer-other", Partition:"partition-other", Action:"allow", Permissions:v1alpha1.IntentionPermissions(nil), Description:""}: Both source.peer and source.partition cannot be set.`,
				`spec.sources[1]: Invalid value: v1alpha1.SourceIntention{Name:"db", Namespace:"namespace-c", Peer:"peer-2", Partition:"partition-2", Action:"deny", Permissions:v1alpha1.IntentionPermissions(nil), Description:""}: Both source.peer and source.partition cannot be set.`,
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
