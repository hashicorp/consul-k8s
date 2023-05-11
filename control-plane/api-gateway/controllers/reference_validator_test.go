package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
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

	basicValidReferenceGrant := &gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: toNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      GatewayKind,
					Namespace: fromNamespace,
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

	var secretRefGroup gwv1beta1.Group
	secretRefGroup = Group

	var secretRefKind gwv1beta1.Kind
	secretRefKind = GatewayKind

	var secretRefNamespace gwv1beta1.Namespace
	secretRefNamespace = toNamespace

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		gateway            gwv1beta1.Gateway
		secret             gwv1beta1.SecretObjectReference
		k8sReferenceGrants []runtime.Object
	}{
		"gateway allowed to secret": {
			canReference: true,
			err:          nil,
			ctx:          context.TODO(),
			gateway: gwv1beta1.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       HTTPRouteKind,
					APIVersion: Group + V1Beta1,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fromNamespace,
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
			canReference, err := rv.GatewayCanReferenceSecret(tc.ctx, tc.gateway, tc.secret)

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.canReference, canReference)
		})
	}
}

func TestHTTPRouteCanReferenceGateway(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("mygateway")

	basicValidReferenceGrant := &gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: toNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      HTTPRouteKind,
					Namespace: fromNamespace,
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

	var gatewayRefGroup gwv1beta1.Group
	gatewayRefGroup = Group

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
					Namespace: fromNamespace,
				},
				Spec:   gwv1beta1.HTTPRouteSpec{},
				Status: gwv1beta1.HTTPRouteStatus{},
			},
			gatewayRef: gwv1beta1.ParentReference{
				Group:       &gatewayRefGroup,
				Kind:        &gatewayRefKind,
				Namespace:   &gatewayRefNamespace,
				Name:        objName,
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

func TestHTTPRouteCanReferenceBackend(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("myBackendRef")

	basicValidReferenceGrant := &gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: toNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      HTTPRouteKind,
					Namespace: fromNamespace,
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

	var backendRefGroup gwv1beta1.Group
	backendRefGroup = Group

	var backendRefKind gwv1beta1.Kind
	backendRefKind = BackendRefKind

	var backendRefNamespace gwv1beta1.Namespace
	backendRefNamespace = toNamespace

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		httpRoute          gwv1beta1.HTTPRoute
		backendRef         gwv1beta1.BackendRef
		k8sReferenceGrants []runtime.Object
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
					Namespace: fromNamespace,
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
			canReference, err := rv.HTTPRouteCanReferenceBackend(tc.ctx, tc.httpRoute, tc.backendRef)

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.canReference, canReference)
		})
	}
}

func TestTCPRouteCanReferenceGateway(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("mygateway")

	basicValidReferenceGrant := &gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: toNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      TCPRouteKind,
					Namespace: fromNamespace,
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

	var gatewayRefGroup gwv1beta1.Group
	gatewayRefGroup = Group

	var gatewayRefKind gwv1beta1.Kind
	gatewayRefKind = GatewayKind

	var gatewayRefNamespace gwv1beta1.Namespace
	gatewayRefNamespace = toNamespace

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		tcpRoute           gwv1alpha2.TCPRoute
		gatewayRef         gwv1beta1.ParentReference
		k8sReferenceGrants []runtime.Object
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
					Namespace: fromNamespace,
				},
				Spec:   gwv1alpha2.TCPRouteSpec{},
				Status: gwv1alpha2.TCPRouteStatus{},
			},
			gatewayRef: gwv1beta1.ParentReference{
				Group:       &gatewayRefGroup,
				Kind:        &gatewayRefKind,
				Namespace:   &gatewayRefNamespace,
				Name:        objName,
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
			canReference, err := rv.TCPRouteCanReferenceGateway(tc.ctx, tc.tcpRoute, tc.gatewayRef)

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.canReference, canReference)
		})
	}
}

func TestTCPRouteCanReferenceBackend(t *testing.T) {
	t.Parallel()

	objName := gwv1beta1.ObjectName("myBackendRef")

	basicValidReferenceGrant := &gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: toNamespace,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group:     Group,
					Kind:      TCPRouteKind,
					Namespace: fromNamespace,
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

	var backendRefGroup gwv1beta1.Group
	backendRefGroup = Group

	var backendRefKind gwv1beta1.Kind
	backendRefKind = BackendRefKind

	var backendRefNamespace gwv1beta1.Namespace
	backendRefNamespace = toNamespace

	cases := map[string]struct {
		canReference       bool
		err                error
		ctx                context.Context
		tcpRoute           gwv1alpha2.TCPRoute
		backendRef         gwv1beta1.BackendRef
		k8sReferenceGrants []runtime.Object
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
					Namespace: fromNamespace,
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
			canReference, err := rv.TCPRouteCanReferenceBackend(tc.ctx, tc.tcpRoute, tc.backendRef)

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
					Group:     Group,
					Kind:      HTTPRouteKind,
					Namespace: fromNamespace,
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
		k8sReferenceGrants []runtime.Object
	}{
		"same namespace": {
			refAllowed: true,
			err:        nil,
			ctx:        context.TODO(),
			fromGK: metav1.GroupKind{
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
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
								Group:     Group,
								Kind:      HTTPRouteKind,
								Namespace: fromNamespace,
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
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
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
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: invalidNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
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
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
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
				Group: Group,
				Kind:  HTTPRouteKind,
			},
			fromNamespace: fromNamespace,
			toGK: metav1.GroupKind{
				Group: Group,
				Kind:  GatewayKind,
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
								Group:     Group,
								Kind:      HTTPRouteKind,
								Namespace: fromNamespace,
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
