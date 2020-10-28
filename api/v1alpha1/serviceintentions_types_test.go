package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul-k8s/api/common"
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
							Action:      "allow",
							Description: "allow access from svc1",
						},
						{
							Name:        "*",
							Namespace:   "not-test",
							Action:      "deny",
							Description: "disallow access from namespace not-test",
						},
						{
							Name:      "svc-2",
							Namespace: "bar",
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
						Action:      "allow",
						Precedence:  0,
						Description: "allow access from svc1",
					},
					{
						Name:        "*",
						Namespace:   "not-test",
						Action:      "deny",
						Precedence:  1,
						Description: "disallow access from namespace not-test",
					},
					{
						Name:      "svc-2",
						Namespace: "bar",
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
							Action:      "allow",
							Description: "allow access from svc1",
						},
						{
							Name:        "*",
							Namespace:   "not-test",
							Action:      "deny",
							Description: "disallow access from namespace not-test",
						},
						{
							Name:      "svc-2",
							Namespace: "bar",
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
						Action:      "allow",
						Description: "allow access from svc1",
					},
					{
						Name:        "*",
						Namespace:   "not-test",
						Action:      "deny",
						Description: "disallow access from namespace not-test",
					},
					{
						Name:      "svc-2",
						Namespace: "bar",
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
			serviceResolver, ok := act.(*capi.ServiceIntentionsConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, serviceResolver)
		})
	}
}

func TestServiceIntentions_AddFinalizer(t *testing.T) {
	serviceResolver := &ServiceIntentions{}
	serviceResolver.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, serviceResolver.ObjectMeta.Finalizers)
}

func TestServiceIntentions_RemoveFinalizer(t *testing.T) {
	serviceResolver := &ServiceIntentions{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	serviceResolver.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, serviceResolver.ObjectMeta.Finalizers)
}

func TestServiceIntentions_SetSyncedCondition(t *testing.T) {
	serviceResolver := &ServiceIntentions{}
	serviceResolver.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, serviceResolver.Status.Conditions[0].Status)
	require.Equal(t, "reason", serviceResolver.Status.Conditions[0].Reason)
	require.Equal(t, "message", serviceResolver.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, serviceResolver.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceIntentions_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			serviceResolver := &ServiceIntentions{
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
	serviceResolver := &ServiceIntentions{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, serviceResolver.GetObjectMeta())
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
func TestServiceIntentions_Default(t *testing.T) {
	namespaceConfig := map[string]struct {
		enabled              bool
		destinationNamespace string
		mirroring            bool
		prefix               string
		sourceNamespace      string
		expectedDestination  string
	}{
		"disabled": {
			enabled:              false,
			destinationNamespace: "",
			mirroring:            false,
			prefix:               "",
			sourceNamespace:      "bar",
			expectedDestination:  "",
		},
		"destinationNS": {
			enabled:              true,
			destinationNamespace: "foo",
			mirroring:            false,
			prefix:               "",
			sourceNamespace:      "bar",
			expectedDestination:  "foo",
		},
		"mirroringEnabledWithoutPrefix": {
			enabled:              true,
			destinationNamespace: "",
			mirroring:            true,
			prefix:               "",
			sourceNamespace:      "bar",
			expectedDestination:  "bar",
		},
		"mirroringWithPrefix": {
			enabled:              true,
			destinationNamespace: "",
			mirroring:            true,
			prefix:               "ns-",
			sourceNamespace:      "bar",
			expectedDestination:  "ns-bar",
		},
	}

	for name, s := range namespaceConfig {
		input := &ServiceIntentions{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: s.sourceNamespace,
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
				Namespace: s.sourceNamespace,
			},
			Spec: ServiceIntentionsSpec{
				Destination: Destination{
					Name:      "bar",
					Namespace: s.expectedDestination,
				},
			},
		}

		t.Run(name, func(t *testing.T) {
			input.Default(s.enabled, s.destinationNamespace, s.mirroring, s.prefix)
			require.True(t, cmp.Equal(input, output))
		})
	}
}

func TestServiceIntentions_Validate(t *testing.T) {
	cases := map[string]struct {
		input          *ServiceIntentions
		expectedErrMsg string
	}{
		"valid": {
			&ServiceIntentions{
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
			"",
		},
		"no sources": {
			&ServiceIntentions{
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources: Required value: at least one source must be specified`,
		},
		"invalid action": {
			&ServiceIntentions{
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].action: Invalid value: "foo": must be one of "allow", "deny"`,
		},
		"invalid permissions.http.pathPrefix": {
			&ServiceIntentions{
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0].pathPrefix: Invalid value: "bar": must begin with a '/'`,
		},
		"invalid permissions.http.pathExact": {
			&ServiceIntentions{
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0].pathExact: Invalid value: "bar": must begin with a '/'`,
		},
		"invalid permissions.action": {
			&ServiceIntentions{
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].permissions[0].action: Invalid value: "foobar": must be one of "allow", "deny"`,
		},
		"both action and permissions specified": {
			&ServiceIntentions{
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0]: Invalid value: "{\"name\":\"svc-2\",\"namespace\":\"bar\",\"action\":\"deny\",\"permissions\":[{\"action\":\"allow\",\"http\":{\"pathExact\":\"/bar\"}}]}": action and permissions are mutually exclusive and only one of them can be specified`,
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

func TestServiceIntentions_ValidateNamespaces(t *testing.T) {
	cases := map[string]struct {
		namespacesEnabled bool
		input             *ServiceIntentions
		expectedErrMsg    string
	}{
		"enabled: valid": {
			true,
			&ServiceIntentions{
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
			"",
		},
		"disabled: destination namespace specified": {
			false,
			&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "foo",
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.destination.namespace: Invalid value: "foo": consul namespaces must be enabled to set destination.namespace`,
		},
		"disabled: single source namespace specified": {
			false,
			&ServiceIntentions{
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
							Namespace: "bar",
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: spec.sources[0].namespace: Invalid value: "bar": consul namespaces must be enabled to set source.namespace`,
		},
		"disabled: multiple source namespace specified": {
			false,
			&ServiceIntentions{
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
							Namespace: "bar",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "baz",
						},
						{
							Name:      "bar",
							Namespace: "baz",
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: [spec.sources[0].namespace: Invalid value: "bar": consul namespaces must be enabled to set source.namespace, spec.sources[1].namespace: Invalid value: "baz": consul namespaces must be enabled to set source.namespace, spec.sources[2].namespace: Invalid value: "baz": consul namespaces must be enabled to set source.namespace]`,
		},
		"disabled: multiple source and destination namespace specified": {
			false,
			&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "does-not-matter",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "dest-service",
						Namespace: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "web",
							Action:    "allow",
							Namespace: "bar",
						},
						{
							Name:      "db",
							Action:    "deny",
							Namespace: "baz",
						},
						{
							Name:      "bar",
							Namespace: "baz",
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
			`serviceintentions.consul.hashicorp.com "does-not-matter" is invalid: [spec.destination.namespace: Invalid value: "foo": consul namespaces must be enabled to set destination.namespace, spec.sources[0].namespace: Invalid value: "bar": consul namespaces must be enabled to set source.namespace, spec.sources[1].namespace: Invalid value: "baz": consul namespaces must be enabled to set source.namespace, spec.sources[2].namespace: Invalid value: "baz": consul namespaces must be enabled to set source.namespace]`,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.ValidateNamespaces(testCase.namespacesEnabled)
			if testCase.expectedErrMsg != "" {
				require.EqualError(t, err, testCase.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
