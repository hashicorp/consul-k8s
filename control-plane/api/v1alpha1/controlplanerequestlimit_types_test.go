// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

func TestControlPlaneRequestLimit_ToConsul(t *testing.T) {
	cases := map[string]struct {
		input    *ControlPlaneRequestLimit
		expected *consul.RateLimitIPConfigEntry
	}{
		"empty fields": {
			&ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode: "disabled",
					ReadWriteRatesConfig: ReadWriteRatesConfig{
						ReadRate:  0,
						WriteRate: 0,
					},
				},
			},
			&consul.RateLimitIPConfigEntry{
				Name: "foo",
				Kind: consul.RateLimitIPConfig,
				Mode: "disabled",
				Meta: map[string]string{
					common.DatacenterKey: "datacenter",
					common.SourceKey:     common.SourceValue,
				},
				ReadRate:  0,
				WriteRate: 0,
			},
		},
		"every field set": {
			&ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ACL: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Catalog: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConfigEntry: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConnectCA: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Coordinate: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					DiscoveryChain: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Health: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Intention: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					KV: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Tenancy: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					PreparedQuery: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Session: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Txn: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
				},
			},
			&consul.RateLimitIPConfigEntry{
				Kind:      consul.RateLimitIPConfig,
				Name:      "foo",
				Mode:      "permissive",
				ReadRate:  100.0,
				WriteRate: 100.0,
				Meta: map[string]string{
					common.DatacenterKey: "datacenter",
					common.SourceKey:     common.SourceValue,
				},
				ACL: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Catalog: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				ConfigEntry: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				ConnectCA: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Coordinate: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				DiscoveryChain: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Health: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Intention: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				KV: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Tenancy: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				PreparedQuery: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Session: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Txn: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
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

func TestControlPlaneRequestLimit_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		internal *ControlPlaneRequestLimit
		consul   consul.ConfigEntry
		matches  bool
	}{
		"empty fields matches": {
			&ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ControlPlaneRequestLimitSpec{},
			},
			&consul.RateLimitIPConfigEntry{
				Kind:        consul.RateLimitIPConfig,
				Name:        "my-test-service",
				Namespace:   "namespace",
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
			&ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ACL: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Catalog: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConfigEntry: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConnectCA: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Coordinate: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					DiscoveryChain: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Health: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Intention: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					KV: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Tenancy: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					PreparedQuery: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Session: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Txn: &ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
				},
			},
			&consul.RateLimitIPConfigEntry{
				Kind:      consul.RateLimitIPConfig,
				Name:      "my-test-service",
				Mode:      "permissive",
				ReadRate:  100.0,
				WriteRate: 100.0,
				Meta: map[string]string{
					common.DatacenterKey: "datacenter",
					common.SourceKey:     common.SourceValue,
				},
				ACL: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Catalog: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				ConfigEntry: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				ConnectCA: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Coordinate: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				DiscoveryChain: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Health: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Intention: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				KV: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Tenancy: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				PreparedQuery: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Session: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
				Txn: &consul.ReadWriteRatesConfig{
					ReadRate:  100.0,
					WriteRate: 100.0,
				},
			},
			true,
		},
		"mismatched types does not match": {
			&ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ControlPlaneRequestLimitSpec{},
			},
			&consul.ProxyConfigEntry{
				Kind:        consul.RateLimitIPConfig,
				Name:        "my-test-service",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			false,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, testCase.matches, testCase.internal.MatchesConsul(testCase.consul))
		})
	}
}

func TestControlPlaneRequestLimit_Validate(t *testing.T) {
	invalidReadWriteRatesConfig := &ReadWriteRatesConfig{
		ReadRate:  -1,
		WriteRate: 0,
	}

	validReadWriteRatesConfig := &ReadWriteRatesConfig{
		ReadRate:  100,
		WriteRate: 100,
	}

	cases := map[string]struct {
		input           *ControlPlaneRequestLimit
		expectedErrMsgs []string
	}{
		"invalid": {
			input: &ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.ControlPlaneRequestLimit,
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode:           "invalid",
					ACL:            invalidReadWriteRatesConfig,
					Catalog:        invalidReadWriteRatesConfig,
					ConfigEntry:    invalidReadWriteRatesConfig,
					ConnectCA:      invalidReadWriteRatesConfig,
					Coordinate:     invalidReadWriteRatesConfig,
					DiscoveryChain: invalidReadWriteRatesConfig,
					Health:         invalidReadWriteRatesConfig,
					Intention:      invalidReadWriteRatesConfig,
					KV:             invalidReadWriteRatesConfig,
					Tenancy:        invalidReadWriteRatesConfig,
					PreparedQuery:  invalidReadWriteRatesConfig,
					Session:        invalidReadWriteRatesConfig,
					Txn:            invalidReadWriteRatesConfig,
				},
			},
			expectedErrMsgs: []string{
				`spec.mode: Invalid value: "invalid": mode must be one of: permissive, enforcing, disabled`,
				`spec.acl.readRate: Invalid value: -1: readRate must be >= 0, spec.acl.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.catalog.readRate: Invalid value: -1: readRate must be >= 0, spec.catalog.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.configEntry.readRate: Invalid value: -1: readRate must be >= 0, spec.configEntry.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.connectCA.readRate: Invalid value: -1: readRate must be >= 0, spec.connectCA.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.coordinate.readRate: Invalid value: -1: readRate must be >= 0, spec.coordinate.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.discoveryChain.readRate: Invalid value: -1: readRate must be >= 0, spec.discoveryChain.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.health.readRate: Invalid value: -1: readRate must be >= 0, spec.health.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.intention.readRate: Invalid value: -1: readRate must be >= 0, spec.intention.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.kv.readRate: Invalid value: -1: readRate must be >= 0, spec.kv.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.tenancy.readRate: Invalid value: -1: readRate must be >= 0, spec.tenancy.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.preparedQuery.readRate: Invalid value: -1: readRate must be >= 0, spec.preparedQuery.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.session.readRate: Invalid value: -1: readRate must be >= 0, spec.session.writeRate: Invalid value: 0: writeRate must be > 0`,
				`spec.txn.readRate: Invalid value: -1: readRate must be >= 0, spec.txn.writeRate: Invalid value: 0: writeRate must be > 0`,
			},
		},
		"valid": {
			input: &ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.ControlPlaneRequestLimit,
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode:                 "permissive",
					ReadWriteRatesConfig: *validReadWriteRatesConfig,
					ACL:                  validReadWriteRatesConfig,
					Catalog:              validReadWriteRatesConfig,
					ConfigEntry:          validReadWriteRatesConfig,
					ConnectCA:            validReadWriteRatesConfig,
					Coordinate:           validReadWriteRatesConfig,
					DiscoveryChain:       validReadWriteRatesConfig,
					Health:               validReadWriteRatesConfig,
					Intention:            validReadWriteRatesConfig,
					KV:                   validReadWriteRatesConfig,
					Tenancy:              validReadWriteRatesConfig,
					PreparedQuery:        validReadWriteRatesConfig,
					Session:              validReadWriteRatesConfig,
					Txn:                  validReadWriteRatesConfig,
				},
			},
			expectedErrMsgs: []string{},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{})
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

func TestControlPlaneRequestLimit_AddFinalizer(t *testing.T) {
	controlPlaneRequestLimit := &ControlPlaneRequestLimit{}
	controlPlaneRequestLimit.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, controlPlaneRequestLimit.ObjectMeta.Finalizers)
}

func TestControlPlaneRequestLimit_RemoveFinalizer(t *testing.T) {
	controlPlaneRequestLimit := &ControlPlaneRequestLimit{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	controlPlaneRequestLimit.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, controlPlaneRequestLimit.ObjectMeta.Finalizers)
}

func TestControlPlaneRequestLimit_SetSyncedCondition(t *testing.T) {
	controlPlaneRequestLimit := &ControlPlaneRequestLimit{}
	controlPlaneRequestLimit.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, controlPlaneRequestLimit.Status.Conditions[0].Status)
	require.Equal(t, "reason", controlPlaneRequestLimit.Status.Conditions[0].Reason)
	require.Equal(t, "message", controlPlaneRequestLimit.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, controlPlaneRequestLimit.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestControlPlaneRequestLimit_SetLastSyncedTime(t *testing.T) {
	controlPlaneRequestLimit := &ControlPlaneRequestLimit{}
	syncedTime := metav1.NewTime(time.Now())
	controlPlaneRequestLimit.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, controlPlaneRequestLimit.Status.LastSyncedTime)
}

func TestControlPlaneRequestLimit_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			controlPlaneRequestLimit := &ControlPlaneRequestLimit{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, controlPlaneRequestLimit.SyncedConditionStatus())
		})
	}
}

func TestControlPlaneRequestLimit_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ControlPlaneRequestLimit{}).GetCondition(ConditionSynced))
}

func TestControlPlaneRequestLimit_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ControlPlaneRequestLimit{}).SyncedConditionStatus())
}

func TestControlPlaneRequestLimit_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ControlPlaneRequestLimit{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestControlPlaneRequestLimit_ConsulKind(t *testing.T) {
	require.Equal(t, consul.RateLimitIPConfig, (&ControlPlaneRequestLimit{}).ConsulKind())
}

func TestControlPlaneRequestLimit_KubeKind(t *testing.T) {
	require.Equal(t, "controlplanerequestlimit", (&ControlPlaneRequestLimit{}).KubeKind())
}

func TestControlPlaneRequestLimit_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ControlPlaneRequestLimit{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestControlPlaneRequestLimit_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ControlPlaneRequestLimit{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestControlPlaneRequestLimit_ConsulNamespace(t *testing.T) {
	require.Equal(t, "default", (&ControlPlaneRequestLimit{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestControlPlaneRequestLimit_ConsulGlobalResource(t *testing.T) {
	require.True(t, (&ControlPlaneRequestLimit{}).ConsulGlobalResource())
}

func TestControlPlaneRequestLimit_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	controlPlaneRequestLimit := &ControlPlaneRequestLimit{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, controlPlaneRequestLimit.GetObjectMeta())
}
