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

func TestValidateServiceExports(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *ServiceExports
		expAllow          bool
		expErrMessage     string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
				Spec: ServiceExportsSpec{},
			},
			expAllow: true,
		},
		"serviceexports exists": {
			existingResources: []runtime.Object{&ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
			}},
			newResource: &ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
				Spec: ServiceExportsSpec{
					Services: []ExportedService{
						{
							Name:      "service",
							Namespace: "service-ns",
							Consumers: []ServiceConsumer{{Partition: "other"}},
						},
					},
				},
			},
			expAllow:      false,
			expErrMessage: "serviceexports resource already defined - only one serviceexports entry is supported",
		},
		"name not exports": {
			existingResources: []runtime.Object{},
			newResource: &ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: "local",
				},
			},
			expAllow:      false,
			expErrMessage: "serviceexports resource name must be \"exports\"",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &ServiceExports{}, &ServiceExportsList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &ServiceExportsWebhook{
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
