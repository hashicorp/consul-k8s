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
						Name: "svc-name",
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
					},
				},
			},
			Exp: &capi.ServiceIntentionsConfigEntry{
				Kind: capi.ServiceIntentions,
				Name: "svc-name",
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

func TestServiceIntentions_Default(t *testing.T) {
	cases := map[string]struct {
		input  *ServiceIntentions
		output *ServiceIntentions
	}{
		"destination.namespace blank, meta.namespace default": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "bar",
					},
				},
			},
			output: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "default",
					},
				},
			},
		},
		"destination.namespace blank, meta.namespace foobar": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "foobar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "bar",
					},
				},
			},
			output: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "foobar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "foobar",
					},
				},
			},
		},
		"sources.namespace blank, meta.namespace default": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:   "baz",
							Action: "allow",
						},
					},
				},
			},
			output: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "baz",
							Action:    "allow",
							Namespace: "default",
						},
					},
				},
			},
		},
		"sources.namespace blank, meta.namespace foobar": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "foobar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:   "baz",
							Action: "allow",
						},
					},
				},
			},
			output: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "foobar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "baz",
							Action:    "allow",
							Namespace: "foobar",
						},
					},
				},
			},
		},
		"only populated blank namespaces": {
			input: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "foobar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:   "baz",
							Action: "allow",
						},
						{
							Name:      "baz2",
							Action:    "allow",
							Namespace: "another-namespace",
						},
					},
				},
			},
			output: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "foobar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "bar",
						Namespace: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "baz",
							Action:    "allow",
							Namespace: "foobar",
						},
						{
							Name:      "baz2",
							Action:    "allow",
							Namespace: "another-namespace",
						},
					},
				},
			},
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			testCase.input.Default()
			require.True(t, cmp.Equal(testCase.input, testCase.output))
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
					},
				},
			},
			"",
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
