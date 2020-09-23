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

func TestValidateConfigEntry(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *ProxyDefaults
		expAllow          bool
		expErrMessage     string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{},
			},
			expAllow: true,
		},
		"invalid config": {
			existingResources: nil,
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage("1"),
				},
			},
			expAllow: false,
			// This error message is because the value "1" is valid JSON but is an invalid map
			expErrMessage: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.config: Invalid value: json.RawMessage{0x31}: must be valid map value",
		},
		"proxy default exists": {
			existingResources: []runtime.Object{&ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
			}},
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					MeshGateway: MeshGatewayConfig{
						Mode: "local",
					},
				},
			},
			expAllow:      false,
			expErrMessage: "proxydefaults resource already defined in cluster. Currently, only one global entry is supported",
		},
		"name not global": {
			existingResources: []runtime.Object{},
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "local",
				},
			},
			expAllow:      false,
			expErrMessage: "proxydefaults resource name must be \"global\"",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &ProxyDefaults{}, &ProxyDefaultsList{})
			client := fake.NewFakeClientWithScheme(s, c.existingResources...)
			decoder, err := admission.NewDecoder(s)
			require.NoError(t, err)

			validator := &proxyDefaultsValidator{
				Client:       client,
				ConsulClient: nil,
				Logger:       logrtest.TestLogger{T: t},
				decoder:      decoder,
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: v1beta1.AdmissionRequest{
					Name:      c.newResource.Name(),
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
