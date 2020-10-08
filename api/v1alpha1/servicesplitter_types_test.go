package v1alpha1

import (
	"testing"

	"github.com/hashicorp/consul-k8s/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test MatchesConsul.
func TestServiceSplitter_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    ServiceSplitter
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceSplitterSpec{},
			},
			Theirs: &capi.ServiceSplitterConfigEntry{
				Kind:        capi.ServiceSplitter,
				Name:        "name",
				Namespace:   "namespace",
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
			Ours: ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceSplitterSpec{
					Splits: []ServiceSplit{
						{
							Weight:        100,
							Service:       "foo",
							ServiceSubset: "bar",
							Namespace:     "baz",
						},
					},
				},
			},
			Theirs: &capi.ServiceSplitterConfigEntry{
				Name: "name",
				Kind: capi.ServiceSplitter,
				Splits: []capi.ServiceSplit{
					{
						Weight:        100,
						Service:       "foo",
						ServiceSubset: "bar",
						Namespace:     "baz",
					},
				},
			},
			Matches: true,
		},
		"different types does not match": {
			Ours: ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceSplitterSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Kind:        capi.ServiceSplitter,
				Name:        "name",
				Namespace:   "namespace",
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

func TestServiceSplitter_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ServiceSplitter
		Exp  *capi.ServiceSplitterConfigEntry
	}{
		"empty fields": {
			Ours: ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceSplitterSpec{},
			},
			Exp: &capi.ServiceSplitterConfigEntry{
				Name: "name",
				Kind: capi.ServiceSplitter,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceSplitterSpec{
					Splits: []ServiceSplit{
						{
							Weight:        100,
							Service:       "foo",
							ServiceSubset: "bar",
							Namespace:     "baz",
						},
					},
				},
			},
			Exp: &capi.ServiceSplitterConfigEntry{
				Name: "name",
				Kind: capi.ServiceSplitter,
				Splits: []capi.ServiceSplit{
					{
						Weight:        100,
						Service:       "foo",
						ServiceSubset: "bar",
						Namespace:     "baz",
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
			ServiceSplitter, ok := act.(*capi.ServiceSplitterConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, ServiceSplitter)
		})
	}
}

func TestServiceSplitter_AddFinalizer(t *testing.T) {
	ServiceSplitter := &ServiceSplitter{}
	ServiceSplitter.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, ServiceSplitter.ObjectMeta.Finalizers)
}

func TestServiceSplitter_RemoveFinalizer(t *testing.T) {
	ServiceSplitter := &ServiceSplitter{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	ServiceSplitter.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, ServiceSplitter.ObjectMeta.Finalizers)
}

func TestServiceSplitter_SetSyncedCondition(t *testing.T) {
	ServiceSplitter := &ServiceSplitter{}
	ServiceSplitter.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, ServiceSplitter.Status.Conditions[0].Status)
	require.Equal(t, "reason", ServiceSplitter.Status.Conditions[0].Reason)
	require.Equal(t, "message", ServiceSplitter.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, ServiceSplitter.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceSplitter_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			ServiceSplitter := &ServiceSplitter{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, ServiceSplitter.SyncedConditionStatus())
		})
	}
}

func TestServiceSplitter_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ServiceSplitter{}).GetCondition(ConditionSynced))
}

func TestServiceSplitter_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ServiceSplitter{}).SyncedConditionStatus())
}

func TestServiceSplitter_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ServiceSplitter{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestServiceSplitter_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ServiceSplitter, (&ServiceSplitter{}).ConsulKind())
}

func TestServiceSplitter_KubeKind(t *testing.T) {
	require.Equal(t, "servicesplitter", (&ServiceSplitter{}).KubeKind())
}

func TestServiceSplitter_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceSplitter{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceSplitter_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceSplitter{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestServiceSplitter_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&ServiceSplitter{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestServiceSplitter_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&ServiceSplitter{}).ConsulGlobalResource())
}
func TestServiceSplitter_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	ServiceSplitter := &ServiceSplitter{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, ServiceSplitter.GetObjectMeta())
}

func TestServiceSplitter_Validate(t *testing.T) {
	cases := map[string]struct {
		input          *ServiceSplitter
		expectedErrMsg string
	}{
		"valid": {
			input: &ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceSplitterSpec{
					Splits: []ServiceSplit{
						{
							Weight: 99.99,
						},
						{
							Weight: 0.01,
						},
					},
				},
			},
		},

		"valid - splits with 0 weight": {
			input: &ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceSplitterSpec{
					Splits: []ServiceSplit{
						{
							Weight: 50.0,
						},
						{
							Weight: 50,
						},
						{
							Weight: 0.0,
						},
						{
							Weight: 0,
						},
					},
				},
			},
		},
		"sum of weights must be 100": {
			input: &ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceSplitterSpec{
					Splits: []ServiceSplit{
						{
							Weight: 90,
						},
						{
							Weight: 5,
						},
					},
				},
			},
			expectedErrMsg: `servicesplitter.consul.hashicorp.com "foo" is invalid: spec.splits: Invalid value: "[{\"weight\":90},{\"weight\":5}]": the sum of weights across all splits must add up to a 100 percent, but adds up to 95.000000`,
		},
		"weight must be between 0.01 and 100": {
			input: &ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceSplitterSpec{
					Splits: []ServiceSplit{
						{
							Weight: 101,
						},
						{
							Weight: 0.001,
						},
					},
				},
			},
			expectedErrMsg: `servicesplitter.consul.hashicorp.com "foo" is invalid: [spec.splits[0].weight: Invalid value: 101: weight must be a percentage between 0.01 and 100, spec.splits[1].weight: Invalid value: 0.001: weight must be a percentage between 0.01 and 100, spec.splits: Invalid value: "[{\"weight\":101},{\"weight\":0.001}]": the sum of weights across all splits must add up to a 100 percent, but adds up to 101.000999]`,
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
