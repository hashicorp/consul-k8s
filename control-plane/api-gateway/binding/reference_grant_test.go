// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"context"
	"testing"

	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ToNamespace      = "toNamespace"
	FromNamespace    = "fromNamespace"
	InvalidNamespace = "invalidNamespace"
	Group            = "gateway.networking.k8s.io"
	V1Beta1          = "/v1beta1"
	V1Alpha2         = "/v1alpha2"
	HTTPRouteKind    = "HTTPRoute"
	TCPRouteKind     = "TCPRoute"
	GatewayKind      = "Gateway"
	BackendRefKind   = "Service"
	SecretKind       = "Secret"
)

func TestGatewayCanReferenceSecret(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("mysecret")

	basicValidReferenceGrant := gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ToNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      GatewayKind,
					Namespace: FromNamespace,
				},
			},
			To: []gwv1beta1.ReferenceGrantTo{
				{
					Group: Group,
					Kind:  SecretKind,
					Name:  &objName,
				},
			},
		},
	}

	secretRefGroup := gwv1beta1.Group(Group)
	secretRefKind := gwv1beta1.Kind(SecretKind)
	secretRefNamespace := gwv1beta1.Namespace(ToNamespace)

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		gateway            gwv1beta1.Gateway
		secret             gwv1beta1.SecretObjectReference
		k8sReferenceGrants []gwv1beta1.ReferenceGrant
	}{
		"gateway allowed to secret": {
			canReference: true,
			err:          nil,
			ctx:          context.TODO(),
			gateway: gwv1beta1.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       GatewayKind,
					APIVersion: Group + V1Beta1,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: FromNamespace,
				},
				Spec:   gwv1beta1.GatewaySpec{},
				Status: gwv1beta1.GatewayStatus{},
			},
			secret: gwv1beta1.SecretObjectReference{
				Group:     &secretRefGroup,
				Kind:      &secretRefKind,
				Namespace: &secretRefNamespace,
				Name:      objName,
			},
			k8sReferenceGrants: []gwv1beta1.ReferenceGrant{
				basicValidReferenceGrant,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rv := NewReferenceValidator(tc.k8sReferenceGrants)
			canReference := rv.GatewayCanReferenceSecret(tc.gateway, tc.secret)

			require.Equal(t, tc.canReference, canReference)
		})
	}
}

func TestHTTPRouteCanReferenceBackend(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("myBackendRef")

	basicValidReferenceGrant := gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ToNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      HTTPRouteKind,
					Namespace: FromNamespace,
				},
			},
			To: []gwv1beta1.ReferenceGrantTo{
				{
					Group: Group,
					Kind:  BackendRefKind,
					Name:  &objName,
				},
			},
		},
	}

	backendRefGroup := gwv1beta1.Group(Group)
	backendRefKind := gwv1beta1.Kind(BackendRefKind)
	backendRefNamespace := gwv1beta1.Namespace(ToNamespace)

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		httpRoute          gwv1beta1.HTTPRoute
		backendRef         gwv1beta1.BackendRef
		k8sReferenceGrants []gwv1beta1.ReferenceGrant
	}{
		"httproute allowed to gateway": {
			canReference: true,
			err:          nil,
			ctx:          context.TODO(),
			httpRoute: gwv1beta1.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind:       HTTPRouteKind,
					APIVersion: Group + V1Beta1,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: FromNamespace,
				},
				Spec:   gwv1beta1.HTTPRouteSpec{},
				Status: gwv1beta1.HTTPRouteStatus{},
			},
			backendRef: gwv1beta1.BackendRef{
				BackendObjectReference: gwv1beta1.BackendObjectReference{
					Group:     &backendRefGroup,
					Kind:      &backendRefKind,
					Name:      objName,
					Namespace: &backendRefNamespace,
					Port:      nil,
				},
				Weight: nil,
			},
			k8sReferenceGrants: []gwv1beta1.ReferenceGrant{
				basicValidReferenceGrant,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rv := NewReferenceValidator(tc.k8sReferenceGrants)
			canReference := rv.HTTPRouteCanReferenceBackend(tc.httpRoute, tc.backendRef)

			require.Equal(t, tc.canReference, canReference)
		})
	}
}

func TestTCPRouteCanReferenceBackend(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("myBackendRef")

	basicValidReferenceGrant := gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ToNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      TCPRouteKind,
					Namespace: FromNamespace,
				},
			},
			To: []gwv1beta1.ReferenceGrantTo{
				{
					Group: Group,
					Kind:  BackendRefKind,
					Name:  &objName,
				},
			},
		},
	}

	backendRefGroup := gwv1beta1.Group(Group)
	backendRefKind := gwv1beta1.Kind(BackendRefKind)
	backendRefNamespace := gwv1beta1.Namespace(ToNamespace)

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		tcpRoute           gwv1alpha2.TCPRoute
		backendRef         gwv1beta1.BackendRef
		k8sReferenceGrants []gwv1beta1.ReferenceGrant
	}{
		"tcpRoute allowed to gateway": {
			canReference: true,
			err:          nil,
			ctx:          context.TODO(),
			tcpRoute: gwv1alpha2.TCPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind:       TCPRouteKind,
					APIVersion: Group + V1Alpha2,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: FromNamespace,
				},
				Spec:   gwv1alpha2.TCPRouteSpec{},
				Status: gwv1alpha2.TCPRouteStatus{},
			},
			backendRef: gwv1beta1.BackendRef{
				BackendObjectReference: gwv1beta1.BackendObjectReference{
					Group:     &backendRefGroup,
					Kind:      &backendRefKind,
					Name:      objName,
					Namespace: &backendRefNamespace,
					Port:      nil,
				},
				Weight: nil,
			},
			k8sReferenceGrants: []gwv1beta1.ReferenceGrant{
				basicValidReferenceGrant,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rv := NewReferenceValidator(tc.k8sReferenceGrants)
			canReference := rv.TCPRouteCanReferenceBackend(tc.tcpRoute, tc.backendRef)

			require.Equal(t, tc.canReference, canReference)
		})
	}
}

func TestReferenceAllowed(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("myObject")

	basicValidReferenceGrant := gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ToNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      HTTPRouteKind,
					Namespace: FromNamespace,
				},
			},
			To: []gwv1beta1.ReferenceGrantTo{
				{
					Group: Group,
					Kind:  GatewayKind,
					Name:  &objName,
				},
			},
		},
	}

	cases := map[string]struct {
		refAllowed         bool
		err                error
		ctx                context.Context
		fromGK             metav1.GroupKind
		fromNamespace      string
		toGK               metav1.GroupKind
		toNamespace        string
		toName             string
		k8sReferenceGrants []gwv1beta1.ReferenceGrant
	}{
		"same namespace": {
			refAllowed: true,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: FromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
			},
			toNamespace: FromNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []gwv1beta1.ReferenceGrant{
				{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: FromNamespace,
					},
					Spec: gwv1beta1.ReferenceGrantSpec{
						From: []gwv1beta1.ReferenceGrantFrom{
							{
								Group:     Group,
								Kind:      HTTPRouteKind,
								Namespace: FromNamespace,
							},
						},
						To: []gwv1beta1.ReferenceGrantTo{
							{
								Group: Group,
								Kind:  GatewayKind,
								Name:  &objName,
							},
						},
					},
				},
			},
		},
		"reference allowed": {
			refAllowed: true,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: FromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
			},
			toNamespace: ToNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []gwv1beta1.ReferenceGrant{
				basicValidReferenceGrant,
			},
		},
		"reference not allowed": {
			refAllowed: false,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: InvalidNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
			},
			toNamespace: ToNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []gwv1beta1.ReferenceGrant{
				basicValidReferenceGrant,
			},
		},
		"no reference grant defined in namespace": {
			refAllowed: false,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: FromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
			},
			toNamespace:        ToNamespace,
			toName:             string(objName),
			k8sReferenceGrants: nil,
		},
		"reference allowed to all objects in namespace": {
			refAllowed: true,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: FromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
			},
			toNamespace: ToNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []gwv1beta1.ReferenceGrant{
				{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ToNamespace,
					},
					Spec: gwv1beta1.ReferenceGrantSpec{
						From: []gwv1beta1.ReferenceGrantFrom{
							{
								Group:     Group,
								Kind:      HTTPRouteKind,
								Namespace: FromNamespace,
							},
						},
						To: []gwv1beta1.ReferenceGrantTo{
							{
								Group: Group,
								Kind:  GatewayKind,
								Name:  nil,
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rv := NewReferenceValidator(tc.k8sReferenceGrants).(*referenceValidator)
			refAllowed := rv.referenceAllowed(tc.fromGK, tc.fromNamespace, tc.toGK, tc.toNamespace, tc.toName)

			require.Equal(t, tc.refAllowed, refAllowed)
		})
	}
}
