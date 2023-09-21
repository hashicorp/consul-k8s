// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/proto-public/pbresource"
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

func TestValidateMeshConfig(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources   []MeshConfig
		newResource         MeshConfig
		enableNamespaces    bool
		nsMirroring         bool
		consulDestinationNS string
		nsMirroringPrefix   string
		expAllow            bool
		expErrMessage       string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &mockMeshConfig{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			expAllow: true,
		},
		"no duplicates, invalid": {
			existingResources: nil,
			newResource: &mockMeshConfig{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         false,
			},
			expAllow:      false,
			expErrMessage: "invalid",
		},
		"duplicate name": {
			existingResources: []MeshConfig{&mockMeshConfig{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockMeshConfig{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			expAllow:      false,
			expErrMessage: "mockkind resource with name \"foo\" is already defined – all mockkind resources must have unique names across namespaces",
		},
		"duplicate name, namespaces enabled": {
			existingResources: []MeshConfig{&mockMeshConfig{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockMeshConfig{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			enableNamespaces: true,
			expAllow:         false,
			expErrMessage:    "mockkind resource with name \"foo\" is already defined – all mockkind resources must have unique names across namespaces",
		},
		"duplicate name, namespaces enabled, mirroring enabled": {
			existingResources: []MeshConfig{&mockMeshConfig{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockMeshConfig{
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

			lister := &mockMeshConfigLister{
				Resources: c.existingResources,
			}
			response := ValidateMeshConfig(ctx, admission.Request{
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
				ConsulTenancyConfig{
					EnableConsulNamespaces:     c.enableNamespaces,
					ConsulDestinationNamespace: c.consulDestinationNS,
					EnableNSMirroring:          c.nsMirroring,
					NSMirroringPrefix:          c.nsMirroringPrefix,
				})
			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}

func TestMeshConfigDefaultingPatches(t *testing.T) {
	meshConfig := &mockMeshConfig{
		MockName: "test",
		Valid:    true,
	}

	// This test validates that DefaultingPatches invokes DefaultNamespaceFields on the Config Entry.
	patches, err := MeshConfigDefaultingPatches(meshConfig, ConsulTenancyConfig{})
	require.NoError(t, err)

	require.Equal(t, []jsonpatch.Operation{
		{
			Operation: "replace",
			Path:      "/MockNamespace",
			Value:     "bar",
		},
	}, patches)
}

type mockMeshConfigLister struct {
	Resources []MeshConfig
}

var _ MeshConfigLister = &mockMeshConfigLister{}

func (in *mockMeshConfigLister) List(_ context.Context) ([]MeshConfig, error) {
	return in.Resources, nil
}

type mockMeshConfig struct {
	MockName      string
	MockNamespace string
	Valid         bool
}

var _ MeshConfig = &mockMeshConfig{}

func (in *mockMeshConfig) ResourceID(_, _ string) *pbresource.ID {
	return nil
}

func (in *mockMeshConfig) Resource(_, _ string) *pbresource.Resource {
	return nil
}

func (in *mockMeshConfig) GetNamespace() string {
	return in.MockNamespace
}

func (in *mockMeshConfig) SetNamespace(namespace string) {
	in.MockNamespace = namespace
}

func (in *mockMeshConfig) GetName() string {
	return in.MockName
}

func (in *mockMeshConfig) SetName(name string) {
	in.MockName = name
}

func (in *mockMeshConfig) GetGenerateName() string {
	return ""
}

func (in *mockMeshConfig) SetGenerateName(_ string) {}

func (in *mockMeshConfig) GetUID() types.UID {
	return ""
}

func (in *mockMeshConfig) SetUID(_ types.UID) {}

func (in *mockMeshConfig) GetResourceVersion() string {
	return ""
}

func (in *mockMeshConfig) SetResourceVersion(_ string) {}

func (in *mockMeshConfig) GetGeneration() int64 {
	return 0
}

func (in *mockMeshConfig) SetGeneration(_ int64) {}

func (in *mockMeshConfig) GetSelfLink() string {
	return ""
}

func (in *mockMeshConfig) SetSelfLink(_ string) {}

func (in *mockMeshConfig) GetCreationTimestamp() metav1.Time {
	return metav1.Time{}
}

func (in *mockMeshConfig) SetCreationTimestamp(_ metav1.Time) {}

func (in *mockMeshConfig) GetDeletionTimestamp() *metav1.Time {
	return nil
}

func (in *mockMeshConfig) SetDeletionTimestamp(_ *metav1.Time) {}

func (in *mockMeshConfig) GetDeletionGracePeriodSeconds() *int64 {
	return nil
}

func (in *mockMeshConfig) SetDeletionGracePeriodSeconds(_ *int64) {}

func (in *mockMeshConfig) GetLabels() map[string]string {
	return nil
}

func (in *mockMeshConfig) SetLabels(_ map[string]string) {}

func (in *mockMeshConfig) GetAnnotations() map[string]string {
	return nil
}

func (in *mockMeshConfig) SetAnnotations(_ map[string]string) {}

func (in *mockMeshConfig) GetFinalizers() []string {
	return nil
}

func (in *mockMeshConfig) SetFinalizers(_ []string) {}

func (in *mockMeshConfig) GetOwnerReferences() []metav1.OwnerReference {
	return nil
}

func (in *mockMeshConfig) SetOwnerReferences(_ []metav1.OwnerReference) {}

func (in *mockMeshConfig) GetClusterName() string {
	return ""
}

func (in *mockMeshConfig) SetClusterName(_ string) {}

func (in *mockMeshConfig) GetManagedFields() []metav1.ManagedFieldsEntry {
	return nil
}

func (in *mockMeshConfig) SetManagedFields(_ []metav1.ManagedFieldsEntry) {}

func (in *mockMeshConfig) KubernetesName() string {
	return in.MockName
}

func (in *mockMeshConfig) GetObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{}
}

func (in *mockMeshConfig) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (in *mockMeshConfig) DeepCopyObject() runtime.Object {
	return in
}

func (in *mockMeshConfig) AddFinalizer(_ string) {}

func (in *mockMeshConfig) RemoveFinalizer(_ string) {}

func (in *mockMeshConfig) Finalizers() []string {
	return nil
}

func (in *mockMeshConfig) KubeKind() string {
	return "mockkind"
}

func (in *mockMeshConfig) SetSyncedCondition(_ corev1.ConditionStatus, _ string, _ string) {}

func (in *mockMeshConfig) SetLastSyncedTime(_ *metav1.Time) {}

func (in *mockMeshConfig) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	return corev1.ConditionTrue, "", ""
}

func (in *mockMeshConfig) SyncedConditionStatus() corev1.ConditionStatus {
	return corev1.ConditionTrue
}

func (in *mockMeshConfig) Validate(_ ConsulTenancyConfig) error {
	if !in.Valid {
		return errors.New("invalid")
	}
	return nil
}

func (in *mockMeshConfig) DefaultNamespaceFields(_ ConsulTenancyConfig) {
	in.MockNamespace = "bar"
}

func (in *mockMeshConfig) MatchesConsul(_ *pbresource.Resource, _, _ string) bool {
	return false
}
