package binding

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestValidateRefs(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		namespace      string
		refs           []gwv1beta1.BackendObjectReference
		services       map[types.NamespacedName]api.CatalogService
		expectedErrors []error
	}{
		"all pass no namespaces": {
			namespace: "test",
			refs:      []gwv1beta1.BackendObjectReference{{Name: "1"}, {Name: "2"}},
			services: map[types.NamespacedName]api.CatalogService{
				{Name: "1", Namespace: "test"}: {},
				{Name: "2", Namespace: "test"}: {},
				{Name: "3", Namespace: "test"}: {},
			},
			expectedErrors: []error{nil, nil},
		},
		"all pass namespaces": {
			namespace: "test",
			refs: []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: pointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2", Namespace: pointerTo[gwv1beta1.Namespace]("other")},
			},
			services: map[types.NamespacedName]api.CatalogService{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "other"}: {},
				{Name: "3", Namespace: "other"}: {},
			},
			expectedErrors: []error{nil, nil},
		},
		"all pass mixed": {
			namespace: "test",
			refs: []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: pointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2"},
			},
			services: map[types.NamespacedName]api.CatalogService{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "test"}:  {},
				{Name: "3", Namespace: "other"}: {},
			},
			expectedErrors: []error{nil, nil},
		},
		"all fail mixed": {
			namespace: "test",
			refs: []gwv1beta1.BackendObjectReference{
				{Name: "1"},
				{Name: "2", Namespace: pointerTo[gwv1beta1.Namespace]("other")},
			},
			services: map[types.NamespacedName]api.CatalogService{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "test"}:  {},
				{Name: "3", Namespace: "other"}: {},
			},
			expectedErrors: []error{errRouteBackendNotFound, errRouteBackendNotFound},
		},
		"all fail no namespaces": {
			namespace: "test",
			refs: []gwv1beta1.BackendObjectReference{
				{Name: "1"},
				{Name: "2"},
			},
			services: map[types.NamespacedName]api.CatalogService{
				{Name: "1", Namespace: "other"}: {},
				{Name: "2", Namespace: "other"}: {},
				{Name: "3", Namespace: "other"}: {},
			},
			expectedErrors: []error{errRouteBackendNotFound, errRouteBackendNotFound},
		},
		"all fail namespaces": {
			namespace: "test",
			refs: []gwv1beta1.BackendObjectReference{
				{Name: "1", Namespace: pointerTo[gwv1beta1.Namespace]("other")},
				{Name: "2", Namespace: pointerTo[gwv1beta1.Namespace]("other")},
			},
			services: map[types.NamespacedName]api.CatalogService{
				{Name: "1", Namespace: "test"}: {},
				{Name: "2", Namespace: "test"}: {},
				{Name: "3", Namespace: "test"}: {},
			},
			expectedErrors: []error{errRouteBackendNotFound, errRouteBackendNotFound},
		},
		"type failures": {
			namespace: "test",
			refs: []gwv1beta1.BackendObjectReference{
				{Name: "1", Group: pointerTo[gwv1beta1.Group]("test")},
				{Name: "2"},
			},
			services: map[types.NamespacedName]api.CatalogService{
				{Name: "1", Namespace: "test"}: {},
				{Name: "2", Namespace: "test"}: {},
				{Name: "3", Namespace: "test"}: {},
			},
			expectedErrors: []error{errRouteInvalidKind, nil},
		},
	} {
		t.Run(name, func(t *testing.T) {
			refs := make([]gwv1beta1.BackendRef, len(tt.refs))
			for i, ref := range tt.refs {
				refs[i] = gwv1beta1.BackendRef{BackendObjectReference: ref}
			}

			actual := validateRefs(tt.namespace, refs, tt.services)
			require.Equal(t, len(actual), len(tt.refs))
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
			require.Equal(t, tt.expected, validateGateway(tt.object).acceptedErr)
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

// // func (m mergedListeners) validateHostname(index int, listener gwv1beta1.Listener) error {
func TestMergedListeners_ValidateHostname(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		mergedListeners mergedListeners
		expected        error
	}{
		"valid": {
			mergedListeners: []mergedListener{
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("1")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("2")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("3")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("4")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("5")}},
				{},
			},
			expected: nil,
		},
		"invalid nil": {
			mergedListeners: []mergedListener{
				{},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("1")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("2")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("3")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("4")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("5")}},
				{},
			},
			expected: errListenerHostnameConflict,
		},
		"invalid set": {
			mergedListeners: []mergedListener{
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("1")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("2")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("3")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("4")}},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("5")}},
				{},
				{listener: gwv1beta1.Listener{Hostname: pointerTo[gwv1beta1.Hostname]("1")}},
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

	for name, tt := range map[string]struct {
		namespace               string
		tls                     *gwv1beta1.GatewayTLSConfig
		certificates            []corev1.Secret
		expectedResolvedRefsErr error
		expectedAcceptedErr     error
	}{
		"no tls": {
			namespace:               "test",
			tls:                     nil,
			certificates:            nil,
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     nil,
		},
		"not supported certificate": {
			namespace: "test",
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo", Namespace: pointerTo[gwv1beta1.Namespace]("other"), Group: pointerTo[gwv1beta1.Group]("test")},
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
		"not found certificate": {
			namespace: "test",
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "zoiks", Namespace: pointerTo[gwv1beta1.Namespace]("other")},
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
			namespace: "test",
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo", Namespace: pointerTo[gwv1beta1.Namespace]("1")},
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
			namespace: "test",
			tls: &gwv1beta1.GatewayTLSConfig{
				Mode: pointerTo(gwv1beta1.TLSModePassthrough),
			},
			certificates:            nil,
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     errListenerNoTLSPassthrough,
		},
		"valid targeted namespace": {
			namespace: "test",
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo", Namespace: pointerTo[gwv1beta1.Namespace]("1")},
					{Name: "bar", Namespace: pointerTo[gwv1beta1.Namespace]("2")},
					{Name: "baz", Namespace: pointerTo[gwv1beta1.Namespace]("3")},
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
			namespace: "test",
			tls: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{
					{Name: "foo"},
					{Name: "bar"},
					{Name: "baz"},
				},
			},
			certificates: []corev1.Secret{
				{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "bar", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "baz", Namespace: "test"}},
			},
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     nil,
		},
		"valid empty certs": {
			namespace:               "test",
			tls:                     &gwv1beta1.GatewayTLSConfig{},
			certificates:            nil,
			expectedResolvedRefsErr: nil,
			expectedAcceptedErr:     nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			actualAcceptedError, actualResolvedRefsError := validateTLS(tt.namespace, tt.tls, tt.certificates)
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
			require.Equal(t, tt.expectedAcceptedErr, validateListeners("", tt.listeners, nil)[0].acceptedErr)
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
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: pointerTo(gwv1beta1.NamespacesFromSame)}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expected:         true,
		},
		"explicit same namespace not allowed": {
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: pointerTo(gwv1beta1.NamespacesFromSame)}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}},
			expected:         false,
		},
		"all namespace allowed": {
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: pointerTo(gwv1beta1.NamespacesFromAll)}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}},
			expected:         true,
		},
		"invalid namespace from not allowed": {
			allowedRoutes:    &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{From: pointerTo[gwv1beta1.FromNamespaces]("other")}},
			gatewayNamespace: "test",
			routeNamespace:   corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expected:         false,
		},
		"labeled namespace allowed": {
			allowedRoutes: &gwv1beta1.AllowedRoutes{Namespaces: &gwv1beta1.RouteNamespaces{
				From:     pointerTo(gwv1beta1.NamespacesFromSelector),
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
				From:     pointerTo(gwv1beta1.NamespacesFromSelector),
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
				From: pointerTo(gwv1beta1.NamespacesFromSelector),
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

// // func routeAllowedForListenerHostname(hostname *gwv1beta1.Hostname, hostnames []gwv1beta1.Hostname) bool {
// func TestRouteAllowedForListenerHostname(t *testing.T) {
// 	t.Parallel()

// 	for name, tt := range map[string]struct {
// 	}{
// 		"": {},
// 	} {
// 		t.Run("name", func(t *testing.T) {

// 		})
// 	}
// }

// // func hostnamesMatch(a gwv1alpha2.Hostname, b gwv1beta1.Hostname) bool {
// func TestHostnamesMatch(t *testing.T) {
// 	t.Parallel()

// 	for name, tt := range map[string]struct {
// 	}{
// 		"": {},
// 	} {
// 		t.Run("name", func(t *testing.T) {

// 		})
// 	}
// }

// // func routeKindIsAllowedForListener(kinds []gwv1beta1.RouteGroupKind, gk schema.GroupKind) bool {
// func TestRouteKindIsAllowedForListener(t *testing.T) {
// 	t.Parallel()

// 	for name, tt := range map[string]struct {
// 	}{
// 		"": {},
// 	} {
// 		t.Run("name", func(t *testing.T) {

// 		})
// 	}
// }

// // func routeKindIsAllowedForListenerExplicit(allowedRoutes *gwv1alpha2.AllowedRoutes, gk schema.GroupKind) bool {
// func TestRouteKindIsAllowedForListenerExplicit(t *testing.T) {
// 	t.Parallel()

// 	for name, tt := range map[string]struct {
// 	}{
// 		"": {},
// 	} {
// 		t.Run("name", func(t *testing.T) {

// 		})
// 	}
// }
