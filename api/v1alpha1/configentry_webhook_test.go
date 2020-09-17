// Note: this test must be in v1alpha1 instead of common because it needs to
// create the concrete types.
package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type Validator interface {
	Handle(context.Context, admission.Request) admission.Response
}

// This test is used to test all config entry webhooks. It ensures that we error
// if a config entry with the same name is created in another kube namespace.
func TestValidateConfigEntry_HandleErrorsIfConfigEntryWithSameNameExists(t *testing.T) {
	otherNS := "other"

	cases := []struct {
		kind          string
		existingCRD   common.ConfigEntryResource
		newCRD        common.ConfigEntryResource
		addKnownTypes func(*runtime.Scheme)
		validator     func(client.Client, *capi.Client, logr.Logger, *admission.Decoder) Validator
	}{
		{
			kind: "ServiceDefaults",
			existingCRD: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
			},
			newCRD: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: otherNS,
				},
			},
			addKnownTypes: func(s *runtime.Scheme) {
				s.AddKnownTypes(GroupVersion, &ServiceDefaults{})
				s.AddKnownTypes(GroupVersion, &ServiceDefaultsList{})
			},
			validator: func(client client.Client, consulClient *capi.Client, logger logr.Logger, decoder *admission.Decoder) Validator {
				v := NewServiceDefaultsValidator(client, consulClient, logger)
				v.InjectDecoder(decoder)
				return v
			},
		},
		{
			kind: "ServiceResolver",
			existingCRD: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
			},
			newCRD: &ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: otherNS,
				},
			},
			addKnownTypes: func(s *runtime.Scheme) {
				s.AddKnownTypes(GroupVersion, &ServiceResolver{})
				s.AddKnownTypes(GroupVersion, &ServiceResolverList{})
			},
			validator: func(client client.Client, consulClient *capi.Client, logger logr.Logger, decoder *admission.Decoder) Validator {
				v := NewServiceResolverValidator(client, consulClient, logger)
				v.InjectDecoder(decoder)
				return v
			},
		},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			s := runtime.NewScheme()
			c.addKnownTypes(s)
			ctx := context.Background()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(t, err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(t, err)

			client := fake.NewFakeClientWithScheme(s, c.existingCRD)

			decoder, err := admission.NewDecoder(scheme.Scheme)
			require.NoError(t, err)
			marshalledRequestObject, err := json.Marshal(c.newCRD)
			require.NoError(t, err)

			validator := c.validator(client, consulClient, logrtest.TestLogger{T: t}, decoder)
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: v1beta1.AdmissionRequest{
					Name:      c.newCRD.Name(),
					Namespace: otherNS,
					Operation: v1beta1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			})
			require.False(t, response.Allowed)
			expErr := fmt.Sprintf("%s resource with name %q is already defined – all %s resources must have unique names across namespaces",
				c.kind, c.existingCRD.Name(), c.kind)
			require.Equal(t, expErr,
				response.AdmissionResponse.Result.Message)
		})
	}
}
