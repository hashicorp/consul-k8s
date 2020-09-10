package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestRun_HandleErrorsIfServiceDefaultsWithSameNameExists(t *testing.T) {
	svcDefaults := &ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: ServiceDefaultsSpec{
			Protocol: "http",
		},
	}
	s := scheme.Scheme
	s.AddKnownTypes(GroupVersion, svcDefaults)
	s.AddKnownTypes(GroupVersion, &ServiceDefaultsList{})
	ctx := context.Background()

	consul, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer consul.Stop()
	consulClient, err := capi.NewClient(&capi.Config{
		Address: consul.HTTPAddr,
	})
	require.NoError(t, err)

	client := fake.NewFakeClientWithScheme(s, svcDefaults)

	validator := &serviceDefaultsValidator{
		Client:       client,
		ConsulClient: consulClient,
		Logger:       logrtest.TestLogger{T: t},
	}

	decoder, err := admission.NewDecoder(scheme.Scheme)
	require.NoError(t, err)
	err = validator.InjectDecoder(decoder)
	require.NoError(t, err)

	requestObject := &ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "other-namespace",
		},
		Spec: ServiceDefaultsSpec{
			Protocol: "http",
		},
	}
	marshalledRequestObject, err := json.Marshal(requestObject)
	require.NoError(t, err)

	response := validator.Handle(ctx, admission.Request{
		AdmissionRequest: v1beta1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   GroupVersion.Group,
				Version: GroupVersion.Version,
				Kind:    "servicedefaults",
			},
			Resource: metav1.GroupVersionResource{
				Group:    GroupVersion.Group,
				Version:  GroupVersion.Version,
				Resource: "servicedefaults",
			},
			RequestKind: &metav1.GroupVersionKind{
				Group:   GroupVersion.Group,
				Version: GroupVersion.Version,
				Kind:    "servicedefaults",
			},
			RequestResource: &metav1.GroupVersionResource{
				Group:    GroupVersion.Group,
				Version:  GroupVersion.Version,
				Resource: "servicedefaults",
			},
			Name:      "foo",
			Namespace: "other-namespace",
			Operation: v1beta1.Create,
			Object: runtime.RawExtension{
				Raw: marshalledRequestObject,
			},
		},
	})
	require.False(t, response.Allowed)
}
