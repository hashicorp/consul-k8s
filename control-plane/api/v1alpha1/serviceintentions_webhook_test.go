// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

func TestHandle_ServiceIntentions_Create(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *ServiceIntentions
		expAllow          bool
		expErrMessage     string
		mirror            bool
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow: true,
			mirror:   false,
		},
		"invalid action": {
			existingResources: nil,
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "fail",
						},
					},
				},
			},
			expAllow:      false,
			mirror:        false,
			expErrMessage: "serviceintentions.consul.hashicorp.com \"foo-intention\" is invalid: spec.sources[0].action: Invalid value: \"fail\": must be one of \"allow\", \"deny\"",
		},
		"intention managing service exists": {
			existingResources: []runtime.Object{&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow:      false,
			mirror:        true,
			expErrMessage: "an existing ServiceIntentions resource has `spec.destination.name: foo` and `spec.destination.namespace: bar`",
		},
		"intention managing service with same name but different namespace with mirroring": {
			existingResources: []runtime.Object{&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "baz",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow:      true,
			mirror:        true,
			expErrMessage: "",
		},
		"intention managing service shares name but different namespace": {
			existingResources: []runtime.Object{&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "baz",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow:      false,
			mirror:        false,
			expErrMessage: "an existing ServiceIntentions resource has `spec.destination.name: foo`",
		},
		"intention managing service shares name": {
			existingResources: []runtime.Object{&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow:      false,
			mirror:        false,
			expErrMessage: "an existing ServiceIntentions resource has `spec.destination.name: foo`",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &ServiceIntentions{}, &ServiceIntentionsList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder := admission.NewDecoder(s)

			validator := &ServiceIntentionsWebhook{
				Client:  client,
				Logger:  logrtest.New(t),
				decoder: decoder,
				ConsulMeta: common.ConsulMeta{
					NamespacesEnabled: true,
					Mirroring:         c.mirror,
				},
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: otherNS,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			})

			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}

func TestHandle_ServiceIntentions_Update(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *ServiceIntentions
		expAllow          bool
		expErrMessage     string
		mirror            bool
	}{
		"valid update": {
			existingResources: []runtime.Object{&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
						{
							Name:      "bar2",
							Namespace: "foo2",
							Action:    "deny",
						},
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow: true,
			mirror:   false,
		},
		"updating name": {
			existingResources: []runtime.Object{&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
						{
							Name:      "bar2",
							Namespace: "foo2",
							Action:    "deny",
						},
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo-bar",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow:      false,
			mirror:        false,
			expErrMessage: "spec.destination.name and spec.destination.namespace are immutable fields for ServiceIntentions",
		},
		"namespace update": {
			existingResources: []runtime.Object{&ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
						{
							Name:      "bar2",
							Namespace: "foo2",
							Action:    "deny",
						},
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar-foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expAllow:      false,
			mirror:        false,
			expErrMessage: "spec.destination.name and spec.destination.namespace are immutable fields for ServiceIntentions",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			marshalledOldRequestObject, err := json.Marshal(c.existingResources[0])
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &ServiceIntentions{}, &ServiceIntentionsList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder := admission.NewDecoder(s)

			validator := &ServiceIntentionsWebhook{
				Client:  client,
				Logger:  logrtest.New(t),
				decoder: decoder,
				ConsulMeta: common.ConsulMeta{
					NamespacesEnabled: true,
					Mirroring:         c.mirror,
				},
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: otherNS,
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
					OldObject: runtime.RawExtension{
						Raw: marshalledOldRequestObject,
					},
				},
			})

			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}

// Test that we return patches to set Consul namespace fields to their defaults.
// This test also tests OSS where we expect no patches since OSS has no
// Consul namespaces.
func TestHandle_ServiceIntentions_Patches(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		newResource *ServiceIntentions
		expPatches  []jsonpatch.Operation
		errMsg      string
	}{
		"all namespace fields set": {
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo-intention",
					Namespace: "bar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:      "baz",
							Namespace: "baz",
							Action:    "allow",
						},
					},
				},
			},
			expPatches: []jsonpatch.Operation{},
			errMsg:     `serviceintentions.consul.hashicorp.com "foo-intention" is invalid: [spec.destination.namespace: Invalid value: "bar": Consul Enterprise namespaces must be enabled to set destination.namespace, spec.sources[0].namespace: Invalid value: "baz": Consul Enterprise namespaces must be enabled to set source.namespace]`,
		},
		"destination.namespace empty": {
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo-intention",
					Namespace: "bar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:      "bar",
							Namespace: "foo",
							Action:    "allow",
						},
					},
				},
			},
			expPatches: []jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/spec/destination/namespace",
					Value:     "bar",
				},
			},
			errMsg: "",
		},
		"destination.namespace empty and sources.namespace empty": {
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo-intention",
					Namespace: "bar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name: "foo",
					},
					Sources: SourceIntentions{
						{
							Name:   "baz",
							Action: "allow",
						},
					},
				},
			},
			expPatches: []jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/spec/destination/namespace",
					Value:     "bar",
				},
			},
			errMsg: "",
		},
		"multiple sources.namespace empty": {
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo-intention",
					Namespace: "bar",
				},
				Spec: ServiceIntentionsSpec{
					Destination: IntentionDestination{
						Name:      "foo",
						Namespace: "bar",
					},
					Sources: SourceIntentions{
						{
							Name:   "baz",
							Action: "allow",
						},
						{
							Name:   "svc",
							Action: "allow",
						},
					},
				},
			},
			expPatches: []jsonpatch.Operation{},
			errMsg:     `serviceintentions.consul.hashicorp.com "foo-intention" is invalid: spec.destination.namespace: Invalid value: "bar": Consul Enterprise namespaces must be enabled to set destination.namespace`,
		},
	}
	for name, c := range cases {
		for _, namespacesEnabled := range []bool{false, true} {
			testName := fmt.Sprintf("%s namespaces-enabled=%t", name, namespacesEnabled)
			t.Run(testName, func(t *testing.T) {
				ctx := context.Background()
				marshalledRequestObject, err := json.Marshal(c.newResource)
				require.NoError(t, err)
				s := runtime.NewScheme()
				s.AddKnownTypes(GroupVersion, &ServiceIntentions{}, &ServiceIntentionsList{})
				client := fake.NewClientBuilder().WithScheme(s).Build()
				decoder := admission.NewDecoder(s)

				validator := &ServiceIntentionsWebhook{
					Client:  client,
					Logger:  logrtest.New(t),
					decoder: decoder,
					ConsulMeta: common.ConsulMeta{
						NamespacesEnabled: namespacesEnabled,
						Mirroring:         true,
					},
				}
				response := validator.Handle(ctx, admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Name:      c.newResource.KubernetesName(),
						Namespace: otherNS,
						Operation: admissionv1.Create,
						Object: runtime.RawExtension{
							Raw: marshalledRequestObject,
						},
					},
				})

				if namespacesEnabled {
					require.Equal(t, true, response.Allowed, response.AdmissionResponse.Result.Message)
					require.ElementsMatch(t, c.expPatches, response.Patches)
				} else {
					if c.errMsg != "" {
						require.Equal(t, false, response.Allowed)
						require.Equal(t, c.errMsg, response.AdmissionResponse.Result.Message)
					}
					// If namespaces are disabled there should be no patches
					// because we don't default any namespace fields.
					require.Len(t, response.Patches, 0)
				}
			})
		}
	}
}
