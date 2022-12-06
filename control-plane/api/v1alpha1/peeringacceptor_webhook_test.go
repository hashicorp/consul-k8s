package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidatePeeringAcceptor(t *testing.T) {
	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *PeeringAcceptor
		expAllow          bool
		expErrMessage     string
	}{
		"valid, unique secret name": {
			existingResources: []runtime.Object{&PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "peer1",
					Namespace: "default",
				},
				Spec: PeeringAcceptorSpec{
					Peer: &Peer{
						Secret: &Secret{
							Name:    "foo",
							Key:     "data",
							Backend: SecretBackendTypeKubernetes,
						},
					},
				},
			}},
			newResource: &PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "peer2",
					Namespace: "default",
				},
				Spec: PeeringAcceptorSpec{
					Peer: &Peer{
						Secret: &Secret{
							Name:    "foo2",
							Key:     "data2",
							Backend: SecretBackendTypeKubernetes,
						},
					},
				},
			},
			expAllow: true,
		},
		"valid, same secret name, different namespace": {
			existingResources: []runtime.Object{&PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "peer1",
					Namespace: "default",
				},
				Spec: PeeringAcceptorSpec{
					Peer: &Peer{
						Secret: &Secret{
							Name:    "foo",
							Key:     "data",
							Backend: SecretBackendTypeKubernetes,
						},
					},
				},
			}},
			newResource: &PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "peer2",
					Namespace: "other",
				},
				Spec: PeeringAcceptorSpec{
					Peer: &Peer{
						Secret: &Secret{
							Name:    "foo",
							Key:     "data",
							Backend: SecretBackendTypeKubernetes,
						},
					},
				},
			},
			expAllow: true,
		},
		"invalid, duplicate secret name and namespace": {
			existingResources: []runtime.Object{&PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "peer1",
					Namespace: "default",
				},
				Spec: PeeringAcceptorSpec{
					Peer: &Peer{
						Secret: &Secret{
							Name:    "foo",
							Key:     "data",
							Backend: SecretBackendTypeKubernetes,
						},
					},
				},
			}},
			newResource: &PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "peer2",
					Namespace: "default",
				},
				Spec: PeeringAcceptorSpec{
					Peer: &Peer{
						Secret: &Secret{
							Name:    "foo",
							Key:     "data",
							Backend: SecretBackendTypeKubernetes,
						},
					},
				},
			},
			expAllow:      false,
			expErrMessage: "an existing PeeringAcceptor resource has the same secret name `name: foo, namespace: default`",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &PeeringAcceptor{}, &PeeringAcceptorList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &PeeringAcceptorWebhook{
				Client:  client,
				Logger:  logrtest.TestLogger{T: t},
				decoder: decoder,
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: "default",
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
