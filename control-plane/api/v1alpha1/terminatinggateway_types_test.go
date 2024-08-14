// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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

func TestTerminatingGateway_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    TerminatingGateway
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: TerminatingGatewaySpec{},
			},
			Theirs: &capi.TerminatingGatewayConfigEntry{
				Kind:      capi.TerminatingGateway,
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
			Ours: TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name:                   "name",
							CAFile:                 "caFile",
							CertFile:               "certFile",
							KeyFile:                "keyFile",
							SNI:                    "sni",
							DisableAutoHostRewrite: true,
						},
						{
							Name: "*",
						},
					},
				},
			},
			Theirs: &capi.TerminatingGatewayConfigEntry{
				Kind:      capi.TerminatingGateway,
				Name:      "name",
				Namespace: "foobar",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				Services: []capi.LinkedService{
					{
						Name:                   "name",
						CAFile:                 "caFile",
						CertFile:               "certFile",
						KeyFile:                "keyFile",
						SNI:                    "sni",
						DisableAutoHostRewrite: true,
					},
					{
						Name: "*",
					},
				},
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: true,
		},
		"different types does not match": {
			Ours: TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: TerminatingGatewaySpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        "name",
				Kind:        capi.TerminatingGateway,
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

func TestTerminatingGateway_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours TerminatingGateway
		Exp  *capi.TerminatingGatewayConfigEntry
	}{
		"empty fields": {
			Ours: TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: TerminatingGatewaySpec{},
			},
			Exp: &capi.TerminatingGatewayConfigEntry{
				Kind: capi.TerminatingGateway,
				Name: "name",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name:     "name",
							CAFile:   "caFile",
							CertFile: "certFile",
							KeyFile:  "keyFile",
							SNI:      "sni",
						},
						{
							Name: "*",
						},
					},
				},
			},
			Exp: &capi.TerminatingGatewayConfigEntry{
				Kind: capi.TerminatingGateway,
				Name: "name",
				Services: []capi.LinkedService{
					{
						Name:     "name",
						CAFile:   "caFile",
						CertFile: "certFile",
						KeyFile:  "keyFile",
						SNI:      "sni",
					},
					{
						Name: "*",
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
			resource, ok := act.(*capi.TerminatingGatewayConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, resource)
		})
	}
}

func TestTerminatingGateway_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *TerminatingGateway
		namespacesEnabled bool
		expectedErrMsgs   []string
	}{
		"certFile set and keyFile not set": {
			input: &TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name:     "foo",
							CertFile: "certFile",
							KeyFile:  "",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.services[0]: Invalid value: "{\"name\":\"foo\",\"certFile\":\"certFile\"}": if certFile or keyFile is set, the other must also be set`,
			},
		},
		"keyFile set and certFile not set": {
			input: &TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name:     "foo",
							KeyFile:  "keyFile",
							CertFile: "",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.services[0]: Invalid value: "{\"name\":\"foo\",\"keyFile\":\"keyFile\"}": if certFile or keyFile is set, the other must also be set`,
			},
		},
		"service.namespace set when namespaces disabled": {
			input: &TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name:      "foo",
							Namespace: "ns",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.services[0].namespace: Invalid value: "ns": Consul Enterprise namespaces must be enabled to set service.namespace`,
			},
		},
		"service.namespace set when namespaces enabled": {
			input: &TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name:      "foo",
							Namespace: "ns",
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs:   []string{},
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
func TestTerminatingGateway_DefaultNamespaceFields(t *testing.T) {
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
			input := &TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name: "foo",
						},
						{
							Name:      "bar",
							Namespace: "other",
						},
					},
				},
			}
			output := &TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: TerminatingGatewaySpec{
					Services: []LinkedService{
						{
							Name:      "foo",
							Namespace: s.expectedDestination,
						},
						{
							Name:      "bar",
							Namespace: "other",
						},
					},
				},
			}
			input.DefaultNamespaceFields(s.consulMeta)
			require.True(t, cmp.Equal(input, output))
		})
	}
}

func TestTerminatingGateway_AddFinalizer(t *testing.T) {
	terminatingGateway := &TerminatingGateway{}
	terminatingGateway.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, terminatingGateway.ObjectMeta.Finalizers)
}

func TestTerminatingGateway_RemoveFinalizer(t *testing.T) {
	terminatingGateway := &TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	terminatingGateway.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, terminatingGateway.ObjectMeta.Finalizers)
}

func TestTerminatingGateway_SetSyncedCondition(t *testing.T) {
	terminatingGateway := &TerminatingGateway{}
	terminatingGateway.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, terminatingGateway.Status.Conditions[0].Status)
	require.Equal(t, "reason", terminatingGateway.Status.Conditions[0].Reason)
	require.Equal(t, "message", terminatingGateway.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, terminatingGateway.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestTerminatingGateway_SetLastSyncedTime(t *testing.T) {
	terminatingGateway := &TerminatingGateway{}
	syncedTime := metav1.NewTime(time.Now())
	terminatingGateway.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, terminatingGateway.Status.LastSyncedTime)
}

func TestTerminatingGateway_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			terminatingGateway := &TerminatingGateway{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, terminatingGateway.SyncedConditionStatus())
		})
	}
}

func TestTerminatingGateway_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&TerminatingGateway{}).GetCondition(ConditionSynced))
}

func TestTerminatingGateway_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&TerminatingGateway{}).SyncedConditionStatus())
}

func TestTerminatingGateway_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&TerminatingGateway{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestTerminatingGateway_ConsulKind(t *testing.T) {
	require.Equal(t, capi.TerminatingGateway, (&TerminatingGateway{}).ConsulKind())
}

func TestTerminatingGateway_KubeKind(t *testing.T) {
	require.Equal(t, "terminatinggateway", (&TerminatingGateway{}).KubeKind())
}

func TestTerminatingGateway_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&TerminatingGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestTerminatingGateway_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&TerminatingGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestTerminatingGateway_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&TerminatingGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestTerminatingGateway_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&TerminatingGateway{}).ConsulGlobalResource())
}

func TestTerminatingGateway_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	terminatingGateway := &TerminatingGateway{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, terminatingGateway.GetObjectMeta())
}
