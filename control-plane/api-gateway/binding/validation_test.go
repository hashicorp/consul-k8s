// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestValidateRefs(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		route           client.Object
		services        map[types.NamespacedName]corev1.Service
		referenceGrants []gwv1beta1.ReferenceGrant
		meshServices    []v1alpha1.MeshService
		expectedErrors  []error
	}{
		"all pass no namespaces": {
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{{Name: "1"}, {Name: "2"}}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "test"}: {},
				{Name: "2", Namespace: "test"}: {},
				{Name: "3", Namespace: "test"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{nil, nil},
		},
		"all fails namespaces no reference grants": {
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "other"}: {},
				{Name: "3", Namespace: "other"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{errRefNotPermitted, errRefNotPermitted},
		},
		"all pass namespaces": {
			referenceGrants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "HTTPRoute", Namespace: gwv1beta1.Namespace("test")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Service"},
					},
				}},
			},
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "other"}: {},
				{Name: "3", Namespace: "other"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{nil, nil},
		},
		"some pass mixed missing reference grants": {
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2"},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "test"}:  {},
				{Name: "3", Namespace: "other"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{errRefNotPermitted, nil},
		},
		"all pass mixed": {
			referenceGrants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "HTTPRoute", Namespace: gwv1beta1.Namespace("test")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Service"},
					},
				}},
			},
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2"},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "test"}:  {},
				{Name: "3", Namespace: "other"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{nil, nil},
		},
		"all fail mixed": {
			referenceGrants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "HTTPRoute", Namespace: gwv1beta1.Namespace("test")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Service"},
					},
				}},
			},
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1"},
				{Name: "2", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "test"}:  {},
				{Name: "3", Namespace: "other"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{errRouteBackendNotFound, errRouteBackendNotFound},
		},
		"all fail no namespaces": {
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1"},
				{Name: "2"},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "other"}: {},
				{Name: "3", Namespace: "other"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{errRouteBackendNotFound, errRouteBackendNotFound},
		},
		"all fail namespaces": {
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "test"}: {},
				{Name: "2", Namespace: "test"}: {},
				{Name: "3", Namespace: "test"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{errRouteBackendNotFound, errRouteBackendNotFound},
		},
		"type failures": {
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{Name: "1", Group: common.PointerTo[gwv1beta1.Group]("test")},
				{Name: "2"},
			}, nil),
			services: map[types.NamespacedName]corev1.Service{
				{Name: "1", Namespace: "test"}: {},
				{Name: "2", Namespace: "test"}: {},
				{Name: "3", Namespace: "test"}: {},
			},
			meshServices:   []v1alpha1.MeshService{},
			expectedErrors: []error{errRouteInvalidKind, nil},
		},
		"mesh services": {
			route: testHTTPRouteBackends("route", "test", []gwv1beta1.BackendObjectReference{
				{
					Name:  "1",
					Group: common.PointerTo(gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup)),
					Kind:  common.PointerTo(gwv1beta1.Kind(v1alpha1.MeshServiceKind)),
				},
			}, nil),
			meshServices: []v1alpha1.MeshService{
				{ObjectMeta: metav1.ObjectMeta{Name: "1", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "2", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "3", Namespace: "test"}},
			},
			expectedErrors: []error{nil},
		},
	} {
		t.Run(name, func(t *testing.T) {
			refs := getRouteBackends(tt.route)
			resources := common.NewResourceMap(common.ResourceTranslator{}, NewReferenceValidator(tt.referenceGrants), logrtest.NewTestLogger(t))
			for _, service := range tt.meshServices {
				resources.AddMeshService(service)
			}
			for id := range tt.services {
				resources.AddService(id, id.Name)
			}

			actual := validateRefs(tt.route, refs, resources)
			require.Equal(t, len(actual), len(tt.expectedErrors))
			for i, err := range tt.expectedErrors {
				require.Equal(t, err, actual[i].err)
			}
		})
	}
}

func TestValidateGateway(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		object   gwv1beta1.Gateway
		expected error
	}{
		"valid": {
			object:   gwv1beta1.Gateway{},
			expected: nil,
		},
		"invalid": {
			object: gwv1beta1.Gateway{Spec: gwv1beta1.GatewaySpec{Addresses: []gwv1beta1.GatewayAddress{
				{Value: "1"},
			}}},
			expected: errGatewayUnsupportedAddress,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, validateGateway(tt.object, nil, nil).acceptedErr)
		})
	}
}

func TestMergedListeners_ValidateProtocol(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		mergedListeners mergedListeners
		expected        error
	}{
		"valid": {
			mergedListeners: []mergedListener{
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
			},
			expected: nil,
		},
		"invalid": {
			mergedListeners: []mergedListener{
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.TCPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
			},
			expected: errListenerProtocolConflict,
		},
		"big list": {
			mergedListeners: []mergedListener{
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPSProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
				{listener: gwv1beta1.Listener{Protocol: gwv1beta1.HTTPProtocolType}},
			},
			expected: errListenerProtocolConflict,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.mergedListeners.validateProtocol())
		})
	}
}

func TestMergedListeners_ValidateHostname(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		mergedListeners mergedListeners
		expected        error
	}{
		"valid": {
			mergedListeners: []mergedListener{
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("1")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("2")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("3")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("4")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("5")}},
				{},
			},
			expected: nil,
		},
		"invalid nil": {
			mergedListeners: []mergedListener{
				{},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("1")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("2")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("3")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("4")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("5")}},
				{},
			},
			expected: errListenerHostnameConflict,
		},
		"invalid set": {
			mergedListeners: []mergedListener{
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("1")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("2")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("3")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("4")}},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("5")}},
				{},
				{listener: gwv1beta1.Listener{Hostname: common.PointerTo[gwv1beta1.Hostname]("1")}},
			},
			expected: errListenerHostnameConflict,
		},
	} {
		t.Run(name, func(t *testing.T) {
			for i, l := range tt.mergedListeners {
				l.index = i
				tt.mergedListeners[i] = l
			}

			require.Equal(t, tt.expected, tt.mergedListeners.validateHostname(0, tt.mergedListeners[0].listener))
		})
	}
}

func TestValidateTLS(t *testing.T) {
	t.Parallel()

	_, secret := generateTestCertificate(t, "", "")

	for name, tt := range map[string]struct {
		gateway                 gwv1beta1.Gateway
		grants                  []gwv1beta1.ReferenceGrant
		tls                     *gwv1beta1.GatewayTLSConfig
		certificates            []corev1.Secret
		expectedResolvedRefsErr error
		expectedAcceptedErr     error
	}{
		"no tls": {
			gateway:                 gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls:                     nil,
			certificates:            nil,
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     nil,
		},
		"not supported certificate": {
			gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo", Namespace: common.PointerTo[gwv1beta1.Namespace]("other"), Group: common.PointerTo[gwv1beta1.Group]("test")},
				},
			},
			certificates: []corev1.Secret{
				{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "bar", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "baz", Namespace: "other"}},
			},
			expectedResolvedRefsErr: errListenerInvalidCertificateRef_NotSupported,
			expectedAcceptedErr:     nil,
		},
		"not allowed certificate": {
			gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
				},
			},
			certificates: []corev1.Secret{
				{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "bar", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "baz", Namespace: "other"}},
			},
			expectedResolvedRefsErr: errRefNotPermitted,
			expectedAcceptedErr:     nil,
		},
		"not found certificate": {
			grants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "Gateway", Namespace: gwv1beta1.Namespace("default")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Secret"},
					},
				}},
			},
			gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "zoiks", Namespace: common.PointerTo[gwv1beta1.Namespace]("other")},
				},
			},
			certificates: []corev1.Secret{
				{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "bar", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "baz", Namespace: "other"}},
			},
			expectedResolvedRefsErr: errListenerInvalidCertificateRef_NotFound,
			expectedAcceptedErr:     nil,
		},
		"not found certificate mismatched namespace": {
			grants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "Gateway", Namespace: gwv1beta1.Namespace("default")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Secret"},
					},
				}},
			},
			gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo", Namespace: common.PointerTo[gwv1beta1.Namespace]("foo")},
				},
			},
			certificates: []corev1.Secret{
				{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "bar", Namespace: "other"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "baz", Namespace: "other"}},
			},
			expectedResolvedRefsErr: errListenerInvalidCertificateRef_NotFound,
			expectedAcceptedErr:     nil,
		},
		"passthrough mode": {
			gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls: &gwv1beta1.GatewayTLSConfig{
				Mode: common.PointerTo(gwv1beta1.TLSModePassthrough),
			},
			certificates:            nil,
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     errListenerNoTLSPassthrough,
		},
		"valid targeted namespace": {
			grants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "1", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "Gateway", Namespace: gwv1beta1.Namespace("default")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Secret"},
					},
				}},
				{ObjectMeta: metav1.ObjectMeta{Namespace: "2", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "Gateway", Namespace: gwv1beta1.Namespace("default")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Secret"},
					},
				}},
				{ObjectMeta: metav1.ObjectMeta{Namespace: "3", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "Gateway", Namespace: gwv1beta1.Namespace("default")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Kind: "Secret"},
					},
				}},
			},
			gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo", Namespace: common.PointerTo[gwv1beta1.Namespace]("1")},
					{Name: "bar", Namespace: common.PointerTo[gwv1beta1.Namespace]("2")},
					{Name: "baz", Namespace: common.PointerTo[gwv1beta1.Namespace]("3")},
				},
			},
			certificates: []corev1.Secret{
				{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "bar", Namespace: "2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "baz", Namespace: "3"}},
			},
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     nil,
		},
		"valid same namespace": {
			gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo"},
					{Name: "bar"},
					{Name: "baz"},
				},
			},
			certificates: []corev1.Secret{
				{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "bar", Namespace: "default"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "baz", Namespace: "default"}},
			},
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     nil,
		},
		"valid empty certs": {
			gateway:                 gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
			tls:                     &gwv1beta1.GatewayTLSConfig{},
			certificates:            nil,
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			resources := common.NewResourceMap(common.ResourceTranslator{}, NewReferenceValidator(tt.grants), logrtest.NewTestLogger(t))
			for _, certificate := range tt.certificates {
				// make the data valid
				certificate.Data = secret.Data
				resources.ReferenceCountCertificate(certificate)
			}

			actualAcceptedError, actualResolvedRefsError := validateTLS(tt.gateway, tt.tls, resources)
			require.Equal(t, tt.expectedResolvedRefsErr, actualResolvedRefsError)
			require.Equal(t, tt.expectedAcceptedErr, actualAcceptedError)
		})
	}
}

func TestValidateListeners(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		listeners           []gwv1beta1.Listener
		expectedAcceptedErr error
	}{
		"valid protocol HTTP": {
			listeners: []gwv1beta1.Listener{
				{Protocol: gwv1beta1.HTTPProtocolType},
			},
			expectedAcceptedErr: nil,
		},
		"valid protocol HTTPS": {
			listeners: []gwv1beta1.Listener{
				{Protocol: gwv1beta1.HTTPSProtocolType},
			},
			expectedAcceptedErr: nil,
		},
		"valid protocol TCP": {
			listeners: []gwv1beta1.Listener{
				{Protocol: gwv1beta1.TCPProtocolType},
			},
			expectedAcceptedErr: nil,
		},
		"invalid protocol UDP": {
			listeners: []gwv1beta1.Listener{
				{Protocol: gwv1beta1.UDPProtocolType},
			},
			expectedAcceptedErr: errListenerUnsupportedProtocol,
		},
		"invalid port": {
			listeners: []gwv1beta1.Listener{
				{Protocol: gwv1beta1.TCPProtocolType, Port: 20000},
			},
			expectedAcceptedErr: errListenerPortUnavailable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expectedAcceptedErr, validateListeners(gatewayWithFinalizer(gwv1beta1.GatewaySpec{}), tt.listeners, nil)[0].acceptedErr)
		})
	}
}

func TestRouteAllowedForListenerNamespaces(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		allowedRoutes    *gwv1beta1.AllowedRoutes
		gatewayNamespace string
		routeNamespace   corev1.Namespace
		expected         bool
	}{
		"default same namespace allowed": {
			allowedRoutes:    nil,
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expected:         true,
		},
		"default same namespace not allowed": {
			allowedRoutes:    nil,
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}},
			expected:         false,
		},
		"explicit same namespace allowed": {
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: common.PointerTo(gwv1beta1.NamespacesFromSame)}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expected:         true,
		},
		"explicit same namespace not allowed": {
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: common.PointerTo(gwv1beta1.NamespacesFromSame)}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}},
			expected:         false,
		},
		"all namespace allowed": {
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: common.PointerTo(gwv1beta1.NamespacesFromAll)}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}},
			expected:         true,
		},
		"invalid namespace from not allowed": {
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: common.PointerTo[gwv1beta1.FromNamespaces]("other")}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expected:         false,
		},
		"labeled namespace allowed": {
			allowedRoutes: &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{
				From:     common.PointerTo(gwv1beta1.NamespacesFromSelector),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
			}},
			gatewayNamespace: "test",
			routeNamespace: corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{
				"foo": "bar",
			}}},
			expected: true,
		},
		"labeled namespace not allowed": {
			allowedRoutes: &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{
				From:     common.PointerTo(gwv1beta1.NamespacesFromSelector),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
			}},
			gatewayNamespace: "test",
			routeNamespace: corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{
				"foo": "baz",
			}}},
			expected: false,
		},
		"invalid labeled namespace": {
			allowedRoutes: &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{
				From: common.PointerTo(gwv1beta1.NamespacesFromSelector),
				Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "foo", Operator: "junk", Values: []string{"1"}},
				}},
			}},
			gatewayNamespace: "test",
			routeNamespace: corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{
				"foo": "bar",
			}}},
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, routeAllowedForListenerNamespaces(tt.gatewayNamespace, tt.allowedRoutes, tt.routeNamespace))
		})
	}
}

func TestRouteAllowedForListenerHostname(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		hostname  *gwv1beta1.Hostname
		hostnames []gwv1beta1.Hostname
		expected  bool
	}{
		"empty hostnames": {
			hostname:  nil,
			hostnames: []gwv1beta1.Hostname{"foo", "bar"},
			expected:  true,
		},
		"empty hostname": {
			hostname:  common.PointerTo[gwv1beta1.Hostname]("foo"),
			hostnames: nil,
			expected:  true,
		},
		"any hostname match": {
			hostname:  common.PointerTo[gwv1beta1.Hostname]("foo"),
			hostnames: []gwv1beta1.Hostname{"foo", "bar"},
			expected:  true,
		},
		"no match": {
			hostname:  common.PointerTo[gwv1beta1.Hostname]("foo"),
			hostnames: []gwv1beta1.Hostname{"bar"},
			expected:  false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, routeAllowedForListenerHostname(tt.hostname, tt.hostnames))
		})
	}
}

func TestHostnamesMatch(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		one      gwv1beta1.Hostname
		two      gwv1beta1.Hostname
		expected bool
	}{
		"wildcard one": {
			one:      "*",
			two:      "foo",
			expected: true,
		},
		"wildcard two": {
			one:      "foo",
			two:      "*",
			expected: true,
		},
		"empty one": {
			one:      "",
			two:      "foo",
			expected: true,
		},
		"empty two": {
			one:      "foo",
			two:      "",
			expected: true,
		},
		"subdomain one": {
			one:      "*.foo",
			two:      "sub.foo",
			expected: true,
		},
		"subdomain two": {
			one:      "sub.foo",
			two:      "*.foo",
			expected: true,
		},
		"exact match": {
			one:      "foo",
			two:      "foo",
			expected: true,
		},
		"no match": {
			one:      "foo",
			two:      "bar",
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, hostnamesMatch(tt.one, tt.two))
		})
	}
}

func TestRouteKindIsAllowedForListener(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		kinds    []gwv1beta1.RouteGroupKind
		gk       schema.GroupKind
		expected bool
	}{
		"empty kinds": {
			kinds:    nil,
			gk:       schema.GroupKind{Group: "a", Kind: "b"},
			expected: true,
		},
		"group specified": {
			kinds: []gwv1beta1.RouteGroupKind{
				{Group: common.PointerTo[gwv1beta1.Group]("a"), Kind: "b"},
			},
			gk:       schema.GroupKind{Group: "a", Kind: "b"},
			expected: true,
		},
		"group unspecified": {
			kinds: []gwv1beta1.RouteGroupKind{
				{Kind: "b"},
			},
			gk:       schema.GroupKind{Group: "a", Kind: "b"},
			expected: true,
		},
		"kind mismatch": {
			kinds: []gwv1beta1.RouteGroupKind{
				{Kind: "b"},
			},
			gk:       schema.GroupKind{Group: "a", Kind: "c"},
			expected: false,
		},
		"group mismatch": {
			kinds: []gwv1beta1.RouteGroupKind{
				{Group: common.PointerTo[gwv1beta1.Group]("a"), Kind: "b"},
			},
			gk:       schema.GroupKind{Group: "d", Kind: "b"},
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, routeKindIsAllowedForListener(tt.kinds, tt.gk))
		})
	}
}
