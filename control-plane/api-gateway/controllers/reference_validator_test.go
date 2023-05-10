package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	toNamespace      = "toNamespace"
	fromNamespace    = "fromNamespace"
	invalidNamespace = "invalidNamespace"
	v1beta1Group     = "v1beta1"
	HTTPRouteKind    = "HTTPRoute"
	GatewayKind      = "Gateway"
)

func TestHTTPRouteCanReferenceGateway(t *testing.T) {
	t.Parallel()

	var objName gwv1beta1.ObjectName
	objName = "barHttpRoute"

	basicValidReferenceGrant := &gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: toNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     v1beta1Group,
					Kind:      HTTPRouteKind,
					Namespace: fromNamespace,
				},
			},
			To: []gwv1beta1.ReferenceGrantTo{
				{
					Group: v1beta1Group,
					Kind:  GatewayKind,
					Name:  &objName,
				},
			},
		},
	}

	var gatewayRefGroup gwv1beta1.Group
	gatewayRefGroup = v1beta1Group

	var gatewayRefKind gwv1beta1.Kind
	gatewayRefKind = GatewayKind

	var gatewayRefNamespace gwv1beta1.Namespace
	gatewayRefNamespace = toNamespace

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		httpRoute          gwv1beta1.HTTPRoute
		gatewayRef         gwv1beta1.ParentReference
		k8sReferenceGrants []runtime.Object
	}{
		"empty namespace on gateway": {
			canReference: true,
			err:          nil,
			ctx:          context.TODO(),
			httpRoute: gwv1beta1.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind:       HTTPRouteKind,
					APIVersion: v1beta1Group, // TODO: where tf does the group go
				},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       gwv1beta1.HTTPRouteSpec{},
				Status:     gwv1beta1.HTTPRouteStatus{},
			},
			gatewayRef: gwv1beta1.ParentReference{
				Group:       &gatewayRefGroup,
				Kind:        &gatewayRefKind,
				Namespace:   &gatewayRefNamespace,
				Name:        "toGateway",
				SectionName: nil,
				Port:        nil,
			},
			k8sReferenceGrants: []runtime.Object{
				basicValidReferenceGrant,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, gwv1beta1.AddToScheme(s))

			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tc.k8sReferenceGrants...).Build()
			rv := NewReferenceValidator(client)
			canReference, err := rv.HTTPRouteCanReferenceGateway(tc.ctx, tc.httpRoute, tc.gatewayRef)

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.canReference, canReference)
		})
	}
}

func TestReferenceAllowed(t *testing.T) {
	t.Parallel()

	var objName gwv1beta1.ObjectName
	objName = "barHttpRoute"

	basicValidReferenceGrant := &gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: toNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     v1beta1Group,
					Kind:      HTTPRouteKind,
					Namespace: fromNamespace,
				},
			},
			To: []gwv1beta1.ReferenceGrantTo{
				{
					Group: v1beta1Group,
					Kind:  "Gateway",
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
		k8sReferenceGrants []runtime.Object
	}{
		"same namespace": {
			refAllowed: true,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  "Gateway",
			},
			toNamespace: fromNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []runtime.Object{
				&gwv1beta1.ReferenceGrant{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: fromNamespace,
					},
					Spec: gwv1beta1.ReferenceGrantSpec{
						From: []gwv1beta1.ReferenceGrantFrom{
							{
								Group:     v1beta1Group,
								Kind:      HTTPRouteKind,
								Namespace: fromNamespace,
							},
						},
						To: []gwv1beta1.ReferenceGrantTo{
							{
								Group: v1beta1Group,
								Kind:  "Gateway",
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
				Group: v1beta1Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  "Gateway",
			},
			toNamespace: toNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []runtime.Object{
				basicValidReferenceGrant,
			},
		},
		"reference not allowed": {
			refAllowed: false,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: invalidNamespace,
			toGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  "Gateway",
			},
			toNamespace: toNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []runtime.Object{
				basicValidReferenceGrant,
			},
		},
		"no reference grant defined in namespace": {
			refAllowed: false,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  "Gateway",
			},
			toNamespace:        toNamespace,
			toName:             string(objName),
			k8sReferenceGrants: nil,
		},
		"reference allowed to all objects in namespace": {
			refAllowed: true,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: v1beta1Group,
				Kind:  "Gateway",
			},
			toNamespace: toNamespace,
			toName:      string(objName),
			k8sReferenceGrants: []runtime.Object{
				&gwv1beta1.ReferenceGrant{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: toNamespace,
					},
					Spec: gwv1beta1.ReferenceGrantSpec{
						From: []gwv1beta1.ReferenceGrantFrom{
							{
								Group:     v1beta1Group,
								Kind:      HTTPRouteKind,
								Namespace: fromNamespace,
							},
						},
						To: []gwv1beta1.ReferenceGrantTo{
							{
								Group: v1beta1Group,
								Kind:  "Gateway",
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
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, gwv1beta1.AddToScheme(s))

			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tc.k8sReferenceGrants...).Build()

			refAllowed, err := referenceAllowed(tc.ctx, tc.fromGK, tc.fromNamespace, tc.toGK, tc.toNamespace, tc.toName, client)

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.refAllowed, refAllowed)
		})
	}
}
