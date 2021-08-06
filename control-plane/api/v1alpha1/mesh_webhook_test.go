package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidateMesh(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *Mesh
		expAllow          bool
		expErrMessage     string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Mesh,
				},
				Spec: MeshSpec{},
			},
			expAllow: true,
		},
		"mesh exists": {
			existingResources: []runtime.Object{&Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Mesh,
				},
			}},
			newResource: &Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Mesh,
				},
				Spec: MeshSpec{
					TransparentProxy: TransparentProxyMeshConfig{
						MeshDestinationsOnly: true,
					},
				},
			},
			expAllow:      false,
			expErrMessage: "mesh resource already defined - only one mesh entry is supported",
		},
		"name not mesh": {
			existingResources: []runtime.Object{},
			newResource: &Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: "local",
				},
			},
			expAllow:      false,
			expErrMessage: "mesh resource name must be \"mesh\"",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &Mesh{}, &MeshList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &MeshWebhook{
				Client:       client,
				ConsulClient: nil,
				Logger:       logrtest.TestLogger{T: t},
				decoder:      decoder,
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
