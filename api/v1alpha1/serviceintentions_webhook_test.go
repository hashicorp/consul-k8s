package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidateServiceIntentions_Create(t *testing.T) {
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
					Destination: Destination{
						Name:      "foo",
						Namespace: "bar",
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
					Destination: Destination{
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
					Destination: Destination{
						Name:      "foo",
						Namespace: "bar",
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "foo",
						Namespace: "bar",
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
					Destination: Destination{
						Name:      "foo",
						Namespace: "bar",
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "foo",
						Namespace: "baz",
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
					Destination: Destination{
						Name:      "foo",
						Namespace: "bar",
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name:      "foo",
						Namespace: "baz",
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
					Destination: Destination{
						Name: "foo",
					},
				},
			}},
			newResource: &ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar-intention",
				},
				Spec: ServiceIntentionsSpec{
					Destination: Destination{
						Name: "foo",
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
			client := fake.NewFakeClientWithScheme(s, c.existingResources...)
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &ServiceIntentionsWebhook{
				Client:                 client,
				ConsulClient:           nil,
				Logger:                 logrtest.TestLogger{T: t},
				decoder:                decoder,
				EnableConsulNamespaces: true,
				EnableNSMirroring:      c.mirror,
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: v1beta1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: otherNS,
					Operation: v1beta1.Create,
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

func TestValidateServiceIntentions_Update(t *testing.T) {
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
					Destination: Destination{
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
					Destination: Destination{
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
					Destination: Destination{
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
					Destination: Destination{
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
					Destination: Destination{
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
					Destination: Destination{
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
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &ServiceIntentions{}, &ServiceIntentionsList{})
			client := fake.NewFakeClientWithScheme(s, c.existingResources...)
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &ServiceIntentionsWebhook{
				Client:                 client,
				ConsulClient:           nil,
				Logger:                 logrtest.TestLogger{T: t},
				decoder:                decoder,
				EnableConsulNamespaces: true,
				EnableNSMirroring:      c.mirror,
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: v1beta1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: otherNS,
					Operation: v1beta1.Update,
					Object: runtime.RawExtension{
						Raw:    marshalledRequestObject,
						Object: c.newResource,
					},
					OldObject: runtime.RawExtension{
						Object: c.existingResources[0],
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
