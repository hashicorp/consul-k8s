// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

func TestSamenessGroups_ToConsul(t *testing.T) {
	cases := map[string]struct {
		input    *SamenessGroup
		expected *capi.SamenessGroupConfigEntry
	}{
		"empty fields": {
			&SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: SamenessGroupSpec{},
			},
			&capi.SamenessGroupConfigEntry{
				Name: "foo",
				Kind: capi.SamenessGroup,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			&SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []SamenessGroupMember{
						{
							Peer: "peer2",
						},
						{
							Partition: "p2",
						},
					},
				},
			},
			&capi.SamenessGroupConfigEntry{
				Name: "foo",
				Kind: capi.SamenessGroup,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				DefaultForFailover: true,
				IncludeLocal:       true,
				Members: []capi.SamenessGroupMember{
					{
						Peer: "peer2",
					},
					{
						Partition: "p2",
					},
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

func TestSamenessGroups_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		internal *SamenessGroup
		consul   capi.ConfigEntry
		matches  bool
	}{
		"empty fields matches": {
			&SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-sameness-group",
				},
				Spec: SamenessGroupSpec{},
			},
			&capi.SamenessGroupConfigEntry{
				Kind:        capi.SamenessGroup,
				Name:        "my-test-sameness-group",
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
			&SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-sameness-group",
				},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []SamenessGroupMember{
						{
							Peer: "peer2",
						},
						{
							Partition: "p2",
						},
						{
							Peer: "test-peer",
						},
					},
				},
			},
			&capi.SamenessGroupConfigEntry{
				Kind: capi.SamenessGroup,
				Name: "my-test-sameness-group",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				DefaultForFailover: true,
				IncludeLocal:       true,
				Members: []capi.SamenessGroupMember{
					{
						Peer: "peer2",
					},
					{
						Partition: "p2",
					},
					{
						Peer:      "test-peer",
						Partition: "default",
					},
				},
			},
			true,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, testCase.matches, testCase.internal.MatchesConsul(testCase.consul))
		})
	}
}

func TestSamenessGroups_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *SamenessGroup
		partitionsEnabled bool
		expectedErrMsg    string
	}{
		"valid": {
			input: &SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-sameness-group",
				},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []SamenessGroupMember{
						{
							Peer:      "peer2",
							Partition: "",
						},
						{
							Peer:      "",
							Partition: "p2",
						},
					},
				},
			},
			partitionsEnabled: true,
			expectedErrMsg:    "",
		},
		"invalid - with peer and partition both": {
			input: &SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-sameness-group",
				},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []SamenessGroupMember{
						{
							Peer:      "peer2",
							Partition: "p2",
						},
					},
				},
			},
			partitionsEnabled: true,
			expectedErrMsg:    "sameness group members cannot specify both partition and peer in the same entry",
		},
		"invalid - no name": {
			input: &SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []SamenessGroupMember{
						{
							Peer: "peer2",
						},
						{
							Partition: "p2",
						},
					},
				},
			},
			partitionsEnabled: true,
			expectedErrMsg:    "sameness groups must have a name defined",
		},
		"invalid - empty members": {
			input: &SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-sameness-group",
				},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members:            []SamenessGroupMember{},
				},
			},
			partitionsEnabled: true,
			expectedErrMsg:    "sameness groups must have at least one member",
		},
		"invalid - not unique members": {
			input: &SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-sameness-group",
				},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []SamenessGroupMember{
						{
							Peer: "peer2",
						},
						{
							Peer: "peer2",
						},
					},
				},
			},
			partitionsEnabled: true,
			expectedErrMsg:    "sameness group members must be unique",
		},
		"invalid - not in default namespace": {
			input: &SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-sameness-group",
					Namespace: "non-default",
				},
				Spec: SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []SamenessGroupMember{
						{
							Peer: "peer2",
						},
					},
				},
			},
			partitionsEnabled: true,
			expectedErrMsg:    "sameness groups must reside in the default namespace",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{})
			if testCase.expectedErrMsg != "" {
				require.ErrorContains(t, err, testCase.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSamenessGroups_GetObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name: "name",
	}
	samenessGroups := &SamenessGroup{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, samenessGroups.GetObjectMeta())
}

func TestSamenessGroups_AddFinalizer(t *testing.T) {
	samenessGroups := &SamenessGroup{}
	samenessGroups.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, samenessGroups.ObjectMeta.Finalizers)
}

func TestSamenessGroups_RemoveFinalizer(t *testing.T) {
	samenessGroups := &SamenessGroup{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	samenessGroups.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, samenessGroups.ObjectMeta.Finalizers)
}

func TestSamenessGroups_ConsulKind(t *testing.T) {
	require.Equal(t, capi.SamenessGroup, (&SamenessGroup{}).ConsulKind())
}

func TestSamenessGroups_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&SamenessGroup{}).ConsulGlobalResource())
}

func TestSamenessGroups_ConsulMirroringNS(t *testing.T) {

}

func TestSamenessGroups_KubeKind(t *testing.T) {
	require.Equal(t, "samenessgroup", (&SamenessGroup{}).KubeKind())
}

func TestSamenessGroups_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&SamenessGroup{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestSamenessGroups_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&SamenessGroup{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestSamenessGroups_SetSyncedCondition(t *testing.T) {
	samenessGroups := &SamenessGroup{}
	samenessGroups.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, samenessGroups.Status.Conditions[0].Status)
	require.Equal(t, "reason", samenessGroups.Status.Conditions[0].Reason)
	require.Equal(t, "message", samenessGroups.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, samenessGroups.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestSamenessGroups_SetLastSyncedTime(t *testing.T) {
	samenessGroups := &SamenessGroup{}
	syncedTime := metav1.NewTime(time.Now())
	samenessGroups.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, samenessGroups.Status.LastSyncedTime)
}

func TestSamenessGroups_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			samenessGroups := &SamenessGroup{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, samenessGroups.SyncedConditionStatus())
		})
	}
}

func TestSamenessGroups_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&SamenessGroup{}).SyncedConditionStatus())
}

func TestSamenessGroups_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&SamenessGroup{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}
