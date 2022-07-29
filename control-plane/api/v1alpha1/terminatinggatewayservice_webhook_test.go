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

func TestValidateTerminatingGatewayService(t *testing.T) {
	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *TerminatingGatewayService
		expAllow          bool
		expErrMessage     string
	}{
		"valid, unique secret name": {
			existingResources: []runtime.Object{&TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service1",
					Namespace: "default",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "foo",
						},
					},
				},
			}},
			newResource: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service2",
					Namespace: "default",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "foo2",
						},
					},
				},
			},
			expAllow: true,
		},
		"valid, same service name, different namespace": {
			existingResources: []runtime.Object{&TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service1",
					Namespace: "default",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "foo",
						},
					},
				},
			}},
			newResource: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service2",
					Namespace: "other",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "foo",
						},
					},
				},
			},
			expAllow: true,
		},
		"invalid, duplicate secret name and namespace": {
			existingResources: []runtime.Object{&TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service1",
					Namespace: "default",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "foo",
						},
					},
				},
			}},
			newResource: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service2",
					Namespace: "default",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "foo",
						},
					},
				},
			},
			expAllow:      false,
			expErrMessage: "an existing TerminatingGatewayService resource has the same service name `name: foo, namespace: default`",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &TerminatingGatewayService{}, &TerminatingGatewayServiceList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &TerminatingGatewayServiceWebhook{
				Client:       client,
				ConsulClient: nil,
				Logger:       logrtest.TestLogger{T: t},
				decoder:      decoder,
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
