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

func TestValidateConsulResource(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources   []ConsulResource
		newResource         ConsulResource
		enableNamespaces    bool
		nsMirroring         bool
		consulDestinationNS string
		nsMirroringPrefix   string
		expAllow            bool
		expErrMessage       string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &mockConsulResource{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			expAllow: true,
		},
		"no duplicates, invalid": {
			existingResources: nil,
			newResource: &mockConsulResource{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         false,
			},
			expAllow:      false,
			expErrMessage: "invalid",
		},
		"duplicate name": {
			existingResources: []ConsulResource{&mockConsulResource{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockConsulResource{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			expAllow:      false,
			expErrMessage: "mockkind resource with name \"foo\" is already defined – all mockkind resources must have unique names across namespaces",
		},
		"duplicate name, namespaces enabled": {
			existingResources: []ConsulResource{&mockConsulResource{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockConsulResource{
				MockName:      "foo",
				MockNamespace: otherNS,
				Valid:         true,
			},
			enableNamespaces: true,
			expAllow:         false,
			expErrMessage:    "mockkind resource with name \"foo\" is already defined – all mockkind resources must have unique names across namespaces",
		},
		"duplicate name, namespaces enabled, mirroring enabled": {
			existingResources: []ConsulResource{&mockConsulResource{
				MockName:      "foo",
				MockNamespace: "default",
			}},
			newResource: &mockConsulResource{
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

			lister := &mockConsulResourceLister{
				Resources: c.existingResources,
			}
			response := ValidateConsulResource(ctx, admission.Request{
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

func TestConsulResourceDefaultingPatches(t *testing.T) {
	meshConfig := &mockConsulResource{
		MockName: "test",
		Valid:    true,
	}

	// This test validates that DefaultingPatches invokes DefaultNamespaceFields on the Config Entry.
	patches, err := ConsulResourceDefaultingPatches(meshConfig, ConsulTenancyConfig{})
	require.NoError(t, err)

	require.Equal(t, []jsonpatch.Operation{
		{
			Operation: "replace",
			Path:      "/MockNamespace",
			Value:     "bar",
		},
	}, patches)
}

type mockConsulResourceLister struct {
	Resources []ConsulResource
}

var _ ConsulResourceLister = &mockConsulResourceLister{}

func (in *mockConsulResourceLister) List(_ context.Context) ([]ConsulResource, error) {
	return in.Resources, nil
}

type mockConsulResource struct {
	MockName      string
	MockNamespace string
	Valid         bool
}

var _ ConsulResource = &mockConsulResource{}

func (in *mockConsulResource) ResourceID(_, _ string) *pbresource.ID {
	return nil
}

func (in *mockConsulResource) Resource(_, _ string) *pbresource.Resource {
	return nil
}

func (in *mockConsulResource) GetNamespace() string {
	return in.MockNamespace
}

func (in *mockConsulResource) SetNamespace(namespace string) {
	in.MockNamespace = namespace
}

func (in *mockConsulResource) GetName() string {
	return in.MockName
}

func (in *mockConsulResource) SetName(name string) {
	in.MockName = name
}

func (in *mockConsulResource) GetGenerateName() string {
	return ""
}

func (in *mockConsulResource) SetGenerateName(_ string) {}

func (in *mockConsulResource) GetUID() types.UID {
	return ""
}

func (in *mockConsulResource) SetUID(_ types.UID) {}

func (in *mockConsulResource) GetResourceVersion() string {
	return ""
}

func (in *mockConsulResource) SetResourceVersion(_ string) {}

func (in *mockConsulResource) GetGeneration() int64 {
	return 0
}

func (in *mockConsulResource) SetGeneration(_ int64) {}

func (in *mockConsulResource) GetSelfLink() string {
	return ""
}

func (in *mockConsulResource) SetSelfLink(_ string) {}

func (in *mockConsulResource) GetCreationTimestamp() metav1.Time {
	return metav1.Time{}
}

func (in *mockConsulResource) SetCreationTimestamp(_ metav1.Time) {}

func (in *mockConsulResource) GetDeletionTimestamp() *metav1.Time {
	return nil
}

func (in *mockConsulResource) SetDeletionTimestamp(_ *metav1.Time) {}

func (in *mockConsulResource) GetDeletionGracePeriodSeconds() *int64 {
	return nil
}

func (in *mockConsulResource) SetDeletionGracePeriodSeconds(_ *int64) {}

func (in *mockConsulResource) GetLabels() map[string]string {
	return nil
}

func (in *mockConsulResource) SetLabels(_ map[string]string) {}

func (in *mockConsulResource) GetAnnotations() map[string]string {
	return nil
}

func (in *mockConsulResource) SetAnnotations(_ map[string]string) {}

func (in *mockConsulResource) GetFinalizers() []string {
	return nil
}

func (in *mockConsulResource) SetFinalizers(_ []string) {}

func (in *mockConsulResource) GetOwnerReferences() []metav1.OwnerReference {
	return nil
}

func (in *mockConsulResource) SetOwnerReferences(_ []metav1.OwnerReference) {}

func (in *mockConsulResource) GetClusterName() string {
	return ""
}

func (in *mockConsulResource) SetClusterName(_ string) {}

func (in *mockConsulResource) GetManagedFields() []metav1.ManagedFieldsEntry {
	return nil
}

func (in *mockConsulResource) SetManagedFields(_ []metav1.ManagedFieldsEntry) {}

func (in *mockConsulResource) KubernetesName() string {
	return in.MockName
}

func (in *mockConsulResource) GetObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{}
}

func (in *mockConsulResource) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (in *mockConsulResource) DeepCopyObject() runtime.Object {
	return in
}

func (in *mockConsulResource) AddFinalizer(_ string) {}

func (in *mockConsulResource) RemoveFinalizer(_ string) {}

func (in *mockConsulResource) Finalizers() []string {
	return nil
}

func (in *mockConsulResource) KubeKind() string {
	return "mockkind"
}

func (in *mockConsulResource) SetSyncedCondition(_ corev1.ConditionStatus, _ string, _ string) {}

func (in *mockConsulResource) SetLastSyncedTime(_ *metav1.Time) {}

func (in *mockConsulResource) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	return corev1.ConditionTrue, "", ""
}

func (in *mockConsulResource) SyncedConditionStatus() corev1.ConditionStatus {
	return corev1.ConditionTrue
}

func (in *mockConsulResource) Validate(_ ConsulTenancyConfig) error {
	if !in.Valid {
		return errors.New("invalid")
	}
	return nil
}

func (in *mockConsulResource) DefaultNamespaceFields(_ ConsulTenancyConfig) {
	in.MockNamespace = "bar"
}

func (in *mockConsulResource) MatchesConsul(_ *pbresource.Resource, _, _ string) bool {
	return false
}
