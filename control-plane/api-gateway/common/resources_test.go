// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestResourceMap_JWTProvider(t *testing.T) {
	resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))
	require.Empty(t, resourceMap.jwtProviders)
	provider := &v1alpha1.JWTProvider{
		TypeMeta: metav1.TypeMeta{
			Kind: "JWTProvider",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-jwt",
		},
		Spec: v1alpha1.JWTProviderSpec{},
	}

	key := api.ResourceReference{
		Name: provider.Name,
		Kind: "JWTProvider",
	}

	resourceMap.AddJWTProvider(provider)

	require.Len(t, resourceMap.jwtProviders, 1)
	require.NotNil(t, resourceMap.jwtProviders[key])
	require.Equal(t, resourceMap.jwtProviders[key], provider)
}

type mockReferenceValidator struct{}

func (m mockReferenceValidator) GatewayCanReferenceSecret(gateway gwv1beta1.Gateway, secretRef gwv1beta1.SecretObjectReference) bool {
	return true
}

func (m mockReferenceValidator) HTTPRouteCanReferenceBackend(httproute gwv1beta1.HTTPRoute, backendRef gwv1beta1.BackendRef) bool {
	return true
}

func (m mockReferenceValidator) TCPRouteCanReferenceBackend(tcpRoute gwv1alpha2.TCPRoute, backendRef gwv1beta1.BackendRef) bool {
	return true
}
