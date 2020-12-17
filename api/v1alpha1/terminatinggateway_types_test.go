package v1alpha1

import (
	"testing"

	"github.com/hashicorp/consul-k8s/api/common"
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
			err := testCase.input.Validate(testCase.namespacesEnabled)
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

func TestTerminatingGateway_AddFinalizer(t *testing.T) {
	resource := &TerminatingGateway{}
	resource.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, resource.ObjectMeta.Finalizers)
}

func TestTerminatingGateway_RemoveFinalizer(t *testing.T) {
	resource := &TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	resource.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, resource.ObjectMeta.Finalizers)
}

func TestTerminatingGateway_SetSyncedCondition(t *testing.T) {
	resource := &TerminatingGateway{}
	resource.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, resource.Status.Conditions[0].Status)
	require.Equal(t, "reason", resource.Status.Conditions[0].Reason)
	require.Equal(t, "message", resource.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, resource.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestTerminatingGateway_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			resource := &TerminatingGateway{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, resource.SyncedConditionStatus())
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
	resource := &TerminatingGateway{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, resource.GetObjectMeta())
}
