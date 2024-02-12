// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidateConfigEntry(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources   []ConfigEntryResource
		newResource         ConfigEntryResource
		enableNamespaces    bool
		nsMirroring         bool
		consulDestinationNS string
		nsMirroringPrefix   string
		expAllow            bool
		expErrMessage       string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &mockConfigEntry{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			expAllow: true,
		},
		"no duplicates, invalid": {
			existingResources: nil,
			newResource: &mockConfigEntry{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         false,
			},
			expAllow:      false,
			expErrMessage: "invalid",
		},
		"duplicate name": {
			existingResources: []ConfigEntryResource{&mockConfigEntry{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockConfigEntry{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			expAllow:      false,
			expErrMessage: "mockkind resource with name \"foo\" is already defined – all mockkind resources must have unique names across namespaces",
		},
		"duplicate name, namespaces enabled": {
			existingResources: []ConfigEntryResource{&mockConfigEntry{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockConfigEntry{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			enableNamespaces: true,
			expAllow:         false,
			expErrMessage:    "mockkind resource with name \"foo\" is already defined – all mockkind resources must have unique names across namespaces",
		},
		"duplicate name, namespaces enabled, mirroring enabled": {
			existingResources: []ConfigEntryResource{&mockConfigEntry{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockConfigEntry{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			enableNamespaces: true,
			nsMirroring:      true,
			expAllow:         true,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)

			lister := &mockConfigEntryLister{
				Resources: c.existingResources,
			}
			response := ValidateConfigEntry(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: otherNS,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			},
				logrtest.New(t),
				lister,
				c.newResource,
				ConsulMeta{
					NamespacesEnabled:    c.enableNamespaces,
					DestinationNamespace: c.consulDestinationNS,
					Mirroring:            c.nsMirroring,
					Prefix:               c.nsMirroringPrefix,
				})
			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}

func TestDefaultingPatches(t *testing.T) {
	cfgEntry := &mockConfigEntry{
		MockName: "test",
		Valid:    true,
	}

	// This test validates that DefaultingPatches invokes DefaultNamespaceFields on the Config Entry.
	patches, err := DefaultingPatches(cfgEntry, ConsulMeta{})
	require.NoError(t, err)

	require.Equal(t, []jsonpatch.Operation{
		{
			Operation: "replace",
			Path:      "/MockNamespace",
			Value:     "bar",
		},
	}, patches)
}

type mockConfigEntryLister struct {
	Resources []ConfigEntryResource
}

func (in *mockConfigEntryLister) List(_ context.Context) ([]ConfigEntryResource, error) {
	return in.Resources, nil
}

type mockConfigEntry struct {
	MockName      string
	MockNamespace string
	Valid         bool
}

func (in *mockConfigEntry) GetNamespace() string {
	return in.MockNamespace
}

func (in *mockConfigEntry) SetNamespace(namespace string) {
	in.MockNamespace = namespace
}

func (in *mockConfigEntry) GetName() string {
	return in.MockName
}

func (in *mockConfigEntry) SetName(name string) {
	in.MockName = name
}

func (in *mockConfigEntry) GetGenerateName() string {
	return ""
}

func (in *mockConfigEntry) SetGenerateName(_ string) {}

func (in *mockConfigEntry) GetUID() types.UID {
	return ""
}

func (in *mockConfigEntry) SetUID(_ types.UID) {}

func (in *mockConfigEntry) GetResourceVersion() string {
	return ""
}

func (in *mockConfigEntry) SetResourceVersion(_ string) {}

func (in *mockConfigEntry) GetGeneration() int64 {
	return 0
}

func (in *mockConfigEntry) SetGeneration(_ int64) {}

func (in *mockConfigEntry) GetSelfLink() string {
	return ""
}

func (in *mockConfigEntry) SetSelfLink(_ string) {}

func (in *mockConfigEntry) GetCreationTimestamp() metav1.Time {
	return metav1.Time{}
}

func (in *mockConfigEntry) SetCreationTimestamp(_ metav1.Time) {}

func (in *mockConfigEntry) GetDeletionTimestamp() *metav1.Time {
	return nil
}

func (in *mockConfigEntry) SetDeletionTimestamp(_ *metav1.Time) {}

func (in *mockConfigEntry) GetDeletionGracePeriodSeconds() *int64 {
	return nil
}

func (in *mockConfigEntry) SetDeletionGracePeriodSeconds(_ *int64) {}

func (in *mockConfigEntry) GetLabels() map[string]string {
	return nil
}

func (in *mockConfigEntry) SetLabels(_ map[string]string) {}

func (in *mockConfigEntry) GetAnnotations() map[string]string {
	return nil
}

func (in *mockConfigEntry) SetAnnotations(_ map[string]string) {}

func (in *mockConfigEntry) GetFinalizers() []string {
	return nil
}

func (in *mockConfigEntry) SetFinalizers(_ []string) {}

func (in *mockConfigEntry) GetOwnerReferences() []metav1.OwnerReference {
	return nil
}

func (in *mockConfigEntry) SetOwnerReferences(_ []metav1.OwnerReference) {}

func (in *mockConfigEntry) GetClusterName() string {
	return ""
}

func (in *mockConfigEntry) SetClusterName(_ string) {}

func (in *mockConfigEntry) GetManagedFields() []metav1.ManagedFieldsEntry {
	return nil
}

func (in *mockConfigEntry) SetManagedFields(_ []metav1.ManagedFieldsEntry) {}

func (in *mockConfigEntry) KubernetesName() string {
	return in.MockName
}

func (in *mockConfigEntry) ConsulMirroringNS() string {
	return in.MockNamespace
}

func (in *mockConfigEntry) GetObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{}
}

func (in *mockConfigEntry) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (in *mockConfigEntry) DeepCopyObject() runtime.Object {
	return in
}

func (in *mockConfigEntry) ConsulGlobalResource() bool {
	return false
}

func (in *mockConfigEntry) AddFinalizer(_ string) {}

func (in *mockConfigEntry) RemoveFinalizer(_ string) {}

func (in *mockConfigEntry) Finalizers() []string {
	return nil
}

func (in *mockConfigEntry) ConsulKind() string {
	return "mock-kind"
}

func (in *mockConfigEntry) KubeKind() string {
	return "mockkind"
}

func (in *mockConfigEntry) ConsulName() string {
	return in.MockName
}

func (in *mockConfigEntry) SetSyncedCondition(_ corev1.ConditionStatus, _ string, _ string) {}

func (in *mockConfigEntry) SetLastSyncedTime(_ *metav1.Time) {}

func (in *mockConfigEntry) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	return corev1.ConditionTrue, "", ""
}

func (in *mockConfigEntry) SyncedConditionStatus() corev1.ConditionStatus {
	return corev1.ConditionTrue
}

func (in *mockConfigEntry) ToConsul(string) capi.ConfigEntry {
	return &capi.ServiceConfigEntry{}
}

func (in *mockConfigEntry) Validate(_ ConsulMeta) error {
	if !in.Valid {
		return errors.New("invalid")
	}
	return nil
}

func (in *mockConfigEntry) DefaultNamespaceFields(_ ConsulMeta) {
	in.MockNamespace = "bar"
}

func (in *mockConfigEntry) MatchesConsul(_ capi.ConfigEntry) bool {
	return false
}
