package common

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidateConfigEntry(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []ConfigEntryResource
		newResource       ConfigEntryResource
		expAllow          bool
		expErrMessage     string
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
				AdmissionRequest: v1beta1.AdmissionRequest{
					Name:      c.newResource.Name(),
					Namespace: otherNS,
					Operation: v1beta1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			},
				logrtest.TestLogger{T: t},
				lister,
				c.newResource)
			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
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

func (in *mockConfigEntry) ConsulNamespaced() bool {
	return true
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

func (in *mockConfigEntry) Name() string {
	return in.MockName
}

func (in *mockConfigEntry) SetSyncedCondition(_ corev1.ConditionStatus, _ string, _ string) {}

func (in *mockConfigEntry) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	return corev1.ConditionTrue, "", ""
}

func (in *mockConfigEntry) SyncedConditionStatus() corev1.ConditionStatus {
	return corev1.ConditionTrue
}

func (in *mockConfigEntry) ToConsul() capi.ConfigEntry {
	return &capi.ServiceConfigEntry{}
}

func (in *mockConfigEntry) Validate() error {
	if !in.Valid {
		return errors.New("invalid")
	}
	return nil
}

func (in *mockConfigEntry) MatchesConsul(_ capi.ConfigEntry) bool {
	return false
}
