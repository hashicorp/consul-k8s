// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	mapset "github.com/deckarep/golang-set"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

// override function for tests.
var timeFunc = metav1.Now

// This is used for any error related to a lack of proper reference grant creation.
var errRefNotPermitted = errors.New("reference not permitted due to lack of ReferenceGrant")

var (
	// Each of the below are specified in the Gateway spec under RouteConditionReason
	// to the RouteConditionReason given in the spec. If a reason is overloaded and can
	// be used with two different types of things (i.e. something is not found or it's not supported)
	// then we distinguish those two usages with errRoute*_Usage.
	errRouteNotAllowedByListeners_Namespace = errors.New("listener does not allow binding routes from the given namespace")
	errRouteNotAllowedByListeners_Protocol  = errors.New("listener does not support route protocol")
	errRouteNoMatchingListenerHostname      = errors.New("listener cannot bind route with a non-aligned hostname")
	errRouteInvalidKind                     = errors.New("invalid backend kind")
	errRouteBackendNotFound                 = errors.New("backend not found")
	errRouteNoMatchingParent                = errors.New("no matching parent")
	errInvalidExternalRefType               = errors.New("invalid externalref filter kind")
	errExternalRefNotFound                  = errors.New("ref not found")
	errFilterInvalid                        = errors.New("filter invalid")
)

// routeValidationResult holds the result of validating a route globally, in other
// words, for a particular backend reference without consideration to its particular
// gateway. Unfortunately, due to the fact that the spec requires a route status be
// associated with a parent reference, what it means is that anything that is global
// in nature, like this status will need to be duplicated for every parent reference
// on a given route status.
type routeValidationResult struct {
	namespace string
	backend   gwv1beta1.BackendRef
	err       error
}

// Type is used for error printing a backend reference type that we don't support on
// a validation error.
func (v routeValidationResult) Type() string {
	return (&metav1.GroupKind{
		Group: common.ValueOr(v.backend.Group, ""),
		Kind:  common.ValueOr(v.backend.Kind, common.KindService),
	}).String()
}

// String is the namespace/name of the reference that has an error.
func (v routeValidationResult) String() string {
	return (types.NamespacedName{Namespace: v.namespace, Name: string(v.backend.Name)}).String()
}

// routeValidationResults contains a list of validation results for the backend references
// on a route.
type routeValidationResults []routeValidationResult

// Condition returns the ResolvedRefs condition that gets duplicated across every relevant
// parent on a route's status.
func (e routeValidationResults) Condition() metav1.Condition {
	// we only use the first error due to the way the spec is structured
	// where you can only have a single condition
	for _, v := range e {
		err := v.err
		if err != nil {
			switch err {
			case errRouteInvalidKind:
				return metav1.Condition{
					Type:    "ResolvedRefs",
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidKind",
					Message: fmt.Sprintf("%s [%s]: %s", v.String(), v.Type(), err.Error()),
				}
			case errRouteBackendNotFound:
				return metav1.Condition{
					Type:    "ResolvedRefs",
					Status:  metav1.ConditionFalse,
					Reason:  "BackendNotFound",
					Message: fmt.Sprintf("%s: %s", v.String(), err.Error()),
				}
			case errRefNotPermitted:
				return metav1.Condition{
					Type:    "ResolvedRefs",
					Status:  metav1.ConditionFalse,
					Reason:  "RefNotPermitted",
					Message: fmt.Sprintf("%s: %s", v.String(), err.Error()),
				}
			default:
				// this should never happen
				return metav1.Condition{
					Type:    "ResolvedRefs",
					Status:  metav1.ConditionFalse,
					Reason:  "UnhandledValidationError",
					Message: err.Error(),
				}
			}
		}
	}
	return metav1.Condition{
		Type:    "ResolvedRefs",
		Status:  metav1.ConditionTrue,
		Reason:  "ResolvedRefs",
		Message: "resolved backend references",
	}
}

// bindResult holds the result of attempting to bind a route to a particular gateway listener
// an error value here means that the route did not bind successfully, no error means that
// the route should be considered bound.
type bindResult struct {
	section gwv1beta1.SectionName
	err     error
}

// bindResults holds the results of attempting to bind a route to a gateway, having a separate
// bindResult for each listener on the gateway.
type bindResults []bindResult

// Error constructs a human readable error for bindResults, containing any errors that a route
// had in binding to a gateway. Note that this is only used if a route failed to bind to every
// listener it attempted to bind to.
func (b bindResults) Error() string {
	messages := []string{}
	for _, result := range b {
		if result.err != nil {
			message := result.err.Error()
			if result.section != "" {
				message = fmt.Sprintf("%s: %s", result.section, result.err.Error())
			}
			messages = append(messages, message)
		}
	}

	sort.Strings(messages)
	return strings.Join(messages, "; ")
}

// DidBind returns whether a route successfully bound to any listener on a gateway.
func (b bindResults) DidBind() bool {
	for _, result := range b {
		if result.err == nil {
			return true
		}
	}
	return false
}

// Condition constructs an Accepted condition for a route that will be scoped
// to the particular parent reference it's using to attempt binding.
func (b bindResults) Condition() metav1.Condition {
	// if we bound to any listeners, say we're accepted
	if b.DidBind() {
		return metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionTrue,
			Reason:  "Accepted",
			Message: "route accepted",
		}
	}

	// default to the most generic reason in the spec "NotAllowedByListeners"
	reason := "NotAllowedByListeners"

	// if we only have a single binding error, we can get more specific
	if len(b) == 1 {
		for _, result := range b {
			switch {
			case errors.Is(result.err, errRouteNoMatchingListenerHostname):
				// if we have a hostname mismatch error, then use the more specific reason
				reason = "NoMatchingListenerHostname"
			case errors.Is(result.err, errRefNotPermitted):
				// or if we have a ref not permitted, then use that
				reason = "RefNotPermitted"
			case errors.Is(result.err, errRouteNoMatchingParent):
				// or if the route declares a parent that we can't find
				reason = "NoMatchingParent"
			case errors.Is(result.err, errExternalRefNotFound):
				reason = "FilterNotFound"
			case errors.Is(result.err, errFilterInvalid):
				reason = "JWTProviderNotFound"
			case errors.Is(result.err, errInvalidExternalRefType):
				reason = "UnsupportedValue"
			}
		}
	}

	return metav1.Condition{
		Type:    "Accepted",
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: b.Error(),
	}
}

// parentBindResult associates a binding result with the given parent reference.
type parentBindResult struct {
	parent  gwv1beta1.ParentReference
	results bindResults
}

// parentBindResults contains the list of all results that occurred when this route
// attempted to bind to a gateway using its parent references.
type parentBindResults []parentBindResult

func (p parentBindResults) boundSections() mapset.Set {
	set := mapset.NewSet()
	for _, result := range p {
		for _, r := range result.results {
			if r.err == nil {
				set.Add(string(r.section))
			}
		}
	}
	return set
}

var (
	// Each of the below are specified in the Gateway spec under ListenerConditionReason.
	// The general usage is that each error is specified as errListener* where * corresponds
	// to the ListenerConditionReason given in the spec. If a reason is overloaded and can
	// be used with two different types of things (i.e. something is not found or it's not supported)
	// then we distinguish those two usages with errListener*_Usage.
	errListenerUnsupportedProtocol                    = errors.New("listener protocol is unsupported")
	errListenerPortUnavailable                        = errors.New("listener port is unavailable")
	errListenerHostnameConflict                       = errors.New("listener hostname conflicts with another listener")
	errListenerProtocolConflict                       = errors.New("listener protocol conflicts with another listener")
	errListenerInvalidCertificateRef_NotFound         = errors.New("certificate not found")
	errListenerInvalidCertificateRef_NotSupported     = errors.New("certificate type is not supported")
	errListenerInvalidCertificateRef_InvalidData      = errors.New("certificate is invalid or does not contain a supported server name")
	errListenerInvalidCertificateRef_NonFIPSRSAKeyLen = errors.New("certificate has an invalid length: RSA Keys must be at least 2048-bit")
	errListenerInvalidCertificateRef_FIPSRSAKeyLen    = errors.New("certificate has an invalid length: RSA keys must be either 2048-bit, 3072-bit, or 4096-bit in FIPS mode")
	errListenerJWTProviderNotFound                    = errors.New("policy referencing this listener references unknown JWT provider")
	errListenerInvalidRouteKinds                      = errors.New("allowed route kind is invalid")
	errListenerProgrammed_Invalid                     = errors.New("listener cannot be programmed because it is invalid")

	// Below is where any custom generic listener validation errors should go.
	// We map anything under here to a custom ListenerConditionReason of Invalid on
	// an Accepted status type.
	errListenerNoTLSPassthrough              = errors.New("TLS passthrough is not supported")
	errListenerTLSCipherSuiteNotConfigurable = errors.New("tls_min_version does not allow tls_cipher_suites configuration")
	errListenerUnsupportedTLSCipherSuite     = errors.New("unsupported cipher suite in tls_cipher_suites")
	errListenerUnsupportedTLSMaxVersion      = errors.New("unsupported tls_max_version")
	errListenerUnsupportedTLSMinVersion      = errors.New("unsupported tls_min_version")

	// This custom listener validation error is used to differentiate between an errListenerPortUnavailable because of
	// direct port conflicts defined by the user (two listeners on the same port) vs a port conflict because we map
	// privileged ports by adding the value passed into the gatewayClassConfig.
	// (i.e. one listener on 80 with a privileged port mapping of 2000, and one listener on 2080 would conflict).
	errListenerMappedToPrivilegedPortMapping = errors.New("listener conflicts with privileged port mapped by GatewayClassConfig privileged port mapping setting")
)

// listenerValidationResult contains the result of internally validating a single listener
// as well as the result of validating it in relation to all its peers (via conflictedErr).
// an error set on any of its members corresponds to an error condition on the corresponding
// status type.
type listenerValidationResult struct {
	// status type: Accepted
	acceptedErr error
	// status type: Conflicted
	conflictedErr error
	// status type: ResolvedRefs
	refErrs []error
	// status type: ResolvedRefs (but with internal validation)
	routeKindErr error
}

// programmedCondition constructs the condition for the Programmed status type.
// If there are no validation errors for the listener, we mark it as programmed.
// If there are validation errors for the listener, we mark it as invalid.
func (l listenerValidationResult) programmedCondition(generation int64) metav1.Condition {
	now := timeFunc()

	switch {
	case l.acceptedErr != nil, l.conflictedErr != nil, len(l.refErrs) != 0, l.routeKindErr != nil:
		return metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			ObservedGeneration: generation,
			Message:            errListenerProgrammed_Invalid.Error(),
			LastTransitionTime: now,
		}
	default:
		return metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionTrue,
			Reason:             "Programmed",
			ObservedGeneration: generation,
			Message:            "listener programmed",
			LastTransitionTime: now,
		}
	}
}

// acceptedCondition constructs the condition for the Accepted status type.
func (l listenerValidationResult) acceptedCondition(generation int64) metav1.Condition {
	now := timeFunc()
	switch l.acceptedErr {
	case errListenerPortUnavailable, errListenerMappedToPrivilegedPortMapping:
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "PortUnavailable",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	case errListenerUnsupportedProtocol:
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "UnsupportedProtocol",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	case nil:
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionTrue,
			Reason:             "Accepted",
			ObservedGeneration: generation,
			Message:            "listener accepted",
			LastTransitionTime: now,
		}
	default:
		// falback to invalid
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	}
}

// conflictedCondition constructs the condition for the Conflicted status type.
func (l listenerValidationResult) conflictedCondition(generation int64) metav1.Condition {
	now := timeFunc()

	switch l.conflictedErr {
	case errListenerProtocolConflict:
		return metav1.Condition{
			Type:               "Conflicted",
			Status:             metav1.ConditionTrue,
			Reason:             "ProtocolConflict",
			ObservedGeneration: generation,
			Message:            l.conflictedErr.Error(),
			LastTransitionTime: now,
		}
	case errListenerHostnameConflict:
		return metav1.Condition{
			Type:               "Conflicted",
			Status:             metav1.ConditionTrue,
			Reason:             "HostnameConflict",
			ObservedGeneration: generation,
			Message:            l.conflictedErr.Error(),
			LastTransitionTime: now,
		}
	default:
		return metav1.Condition{
			Type:               "Conflicted",
			Status:             metav1.ConditionFalse,
			Reason:             "NoConflicts",
			ObservedGeneration: generation,
			Message:            "listener has no conflicts",
			LastTransitionTime: now,
		}
	}
}

// acceptedCondition constructs the condition for the ResolvedRefs status type.
func (l listenerValidationResult) resolvedRefsConditions(generation int64) []metav1.Condition {
	now := timeFunc()

	conditions := make([]metav1.Condition, 0)

	if l.routeKindErr != nil {
		return []metav1.Condition{{
			Type:               "ResolvedRefs",
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidRouteKinds",
			ObservedGeneration: generation,
			Message:            l.routeKindErr.Error(),
			LastTransitionTime: now,
		}}
	}

	for _, refErr := range l.refErrs {
		switch refErr {
		case errListenerInvalidCertificateRef_NotFound,
			errListenerInvalidCertificateRef_NotSupported,
			errListenerInvalidCertificateRef_InvalidData,
			errListenerInvalidCertificateRef_NonFIPSRSAKeyLen,
			errListenerInvalidCertificateRef_FIPSRSAKeyLen:
			conditions = append(conditions, metav1.Condition{
				Type:               "ResolvedRefs",
				Status:             metav1.ConditionFalse,
				Reason:             "InvalidCertificateRef",
				ObservedGeneration: generation,
				Message:            refErr.Error(),
				LastTransitionTime: now,
			})
		case errListenerJWTProviderNotFound:
			conditions = append(conditions, metav1.Condition{
				Type:               "ResolvedRefs",
				Status:             metav1.ConditionFalse,
				Reason:             "InvalidJWTProviderRef",
				ObservedGeneration: generation,
				Message:            refErr.Error(),
				LastTransitionTime: now,
			})
		case errRefNotPermitted:
			conditions = append(conditions, metav1.Condition{
				Type:               "ResolvedRefs",
				Status:             metav1.ConditionFalse,
				Reason:             "RefNotPermitted",
				ObservedGeneration: generation,
				Message:            refErr.Error(),
				LastTransitionTime: now,
			})
		}
	}
	if len(conditions) == 0 {
		conditions = append(conditions, metav1.Condition{
			Type:               "ResolvedRefs",
			Status:             metav1.ConditionTrue,
			Reason:             "ResolvedRefs",
			ObservedGeneration: generation,
			Message:            "resolved references",
			LastTransitionTime: now,
		})
	}
	return conditions
}

// Conditions constructs the entire set of conditions for a given gateway listener.
func (l listenerValidationResult) Conditions(generation int64) []metav1.Condition {
	conditions := []metav1.Condition{
		l.acceptedCondition(generation),
		l.programmedCondition(generation),
		l.conflictedCondition(generation),
	}
	return append(conditions, l.resolvedRefsConditions(generation)...)
}

// listenerValidationResults holds all of the results for a gateway's listeners
// the index of each result needs to correspond exactly to the index of the listener
// on the gateway spec for which it is describing.
type listenerValidationResults []listenerValidationResult

// Invalid returns whether or not there is any listener that is not "Accepted"
// this is used in constructing a gateway's status where the Accepted status
// at the top-level can have a GatewayConditionReason of ListenersNotValid.
func (l listenerValidationResults) Invalid() bool {
	for _, r := range l {
		if r.acceptedErr != nil {
			return true
		}
	}
	return false
}

// Conditions returns the listener conditions at a given index.
func (l listenerValidationResults) Conditions(generation int64, index int) []metav1.Condition {
	result := l[index]
	return result.Conditions(generation)
}

var (
	// Each of the below are specified in the Gateway spec under GatewayConditionReason
	// the general usage is that each error is specified as errGateway* where * corresponds
	// to the GatewayConditionReason given in the spec.
	errGatewayUnsupportedAddress = errors.New("gateway does not support specifying addresses")
	errGatewayListenersNotValid  = errors.New("one or more listeners are invalid")
	errGatewayPending_Pods       = errors.New("gateway pods are still being scheduled")
	errGatewayPending_Consul     = errors.New("gateway configuration is not yet synced to Consul")
)

// gatewayValidationResult contains the result of internally validating a gateway.
// An error set on any of its members corresponds to an error condition on the corresponding
// status type.
type gatewayValidationResult struct {
	acceptedErr   error
	programmedErr error
}

// programmedCondition returns a condition for the Programmed status type.
func (l gatewayValidationResult) programmedCondition(generation int64) metav1.Condition {
	now := timeFunc()

	switch l.programmedErr {
	case errGatewayPending_Pods, errGatewayPending_Consul:
		return metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionFalse,
			Reason:             "Pending",
			ObservedGeneration: generation,
			Message:            l.programmedErr.Error(),
			LastTransitionTime: now,
		}
	default:
		return metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionTrue,
			Reason:             "Programmed",
			ObservedGeneration: generation,
			Message:            "gateway programmed",
			LastTransitionTime: now,
		}
	}
}

// acceptedCondition returns a condition for the Accepted status type. It takes a boolean argument
// for whether or not any of the gateway's listeners are invalid, if they are, it overrides whatever
// Reason is set as an error on the result and instead uses the ListenersNotValid reason.
func (l gatewayValidationResult) acceptedCondition(generation int64, listenersInvalid bool) metav1.Condition {
	now := timeFunc()

	if l.acceptedErr == nil {
		if listenersInvalid {
			return metav1.Condition{
				Type: "Accepted",
				// should one invalid listener cause the entire gateway to become invalid?
				Status:             metav1.ConditionFalse,
				Reason:             "ListenersNotValid",
				ObservedGeneration: generation,
				Message:            errGatewayListenersNotValid.Error(),
				LastTransitionTime: now,
			}
		}

		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionTrue,
			Reason:             "Accepted",
			ObservedGeneration: generation,
			Message:            "gateway accepted",
			LastTransitionTime: now,
		}
	}

	if l.acceptedErr == errGatewayUnsupportedAddress {
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "UnsupportedAddress",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	}

	// fallback to Invalid reason
	return metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionFalse,
		Reason:             "Invalid",
		ObservedGeneration: generation,
		Message:            l.acceptedErr.Error(),
		LastTransitionTime: now,
	}
}

// Conditions constructs the gateway conditions given whether its listeners are valid.
func (l gatewayValidationResult) Conditions(generation int64, listenersInvalid bool) []metav1.Condition {
	return []metav1.Condition{
		l.acceptedCondition(generation, listenersInvalid),
		l.programmedCondition(generation),
	}
}

type gatewayPolicyValidationResult struct {
	acceptedErr      error
	resolvedRefsErrs []error
}

type gatewayPolicyValidationResults []gatewayPolicyValidationResult

var (
	errPolicyListenerReferenceDoesNotExist     = errors.New("gateway policy references a listener that does not exist")
	errPolicyJWTProvidersReferenceDoesNotExist = errors.New("gateway policy references one or more jwt providers that do not exist")
	errNotAcceptedDueToInvalidRefs             = errors.New("policy is not accepted due to errors with references")
)

func (g gatewayPolicyValidationResults) Conditions(generation int64, idx int) []metav1.Condition {
	result := g[idx]
	return result.Conditions(generation)
}

func (g gatewayPolicyValidationResult) Conditions(generation int64) []metav1.Condition {
	return append([]metav1.Condition{g.acceptedCondition(generation)}, g.resolvedRefsConditions(generation)...)
}

func (g gatewayPolicyValidationResult) acceptedCondition(generation int64) metav1.Condition {
	now := timeFunc()
	if g.acceptedErr != nil {
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "ReferencesNotValid",
			ObservedGeneration: generation,
			Message:            g.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	}
	return metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		ObservedGeneration: generation,
		Message:            "gateway policy accepted",
		LastTransitionTime: now,
	}
}

func (g gatewayPolicyValidationResult) resolvedRefsConditions(generation int64) []metav1.Condition {
	now := timeFunc()
	if len(g.resolvedRefsErrs) == 0 {
		return []metav1.Condition{
			{
				Type:               "ResolvedRefs",
				Status:             metav1.ConditionTrue,
				Reason:             "ResolvedRefs",
				ObservedGeneration: generation,
				Message:            "resolved references",
				LastTransitionTime: now,
			},
		}
	}

	conditions := make([]metav1.Condition, 0, len(g.resolvedRefsErrs))
	for _, err := range g.resolvedRefsErrs {
		switch {
		case errors.Is(err, errPolicyListenerReferenceDoesNotExist):
			conditions = append(conditions, metav1.Condition{
				Type:               "ResolvedRefs",
				Status:             metav1.ConditionFalse,
				Reason:             "MissingListenerReference",
				ObservedGeneration: generation,
				Message:            err.Error(),
				LastTransitionTime: now,
			})
		case errors.Is(err, errPolicyJWTProvidersReferenceDoesNotExist):
			conditions = append(conditions, metav1.Condition{
				Type:               "ResolvedRefs",
				Status:             metav1.ConditionFalse,
				Reason:             "MissingJWTProviderReference",
				ObservedGeneration: generation,
				Message:            err.Error(),
				LastTransitionTime: now,
			})
		}
	}
	return conditions
}

type authFilterValidationResults []authFilterValidationResult

type authFilterValidationResult struct {
	acceptedErr    error
	resolvedRefErr error
}

var (
	errRouteFilterJWTProvidersReferenceDoesNotExist = errors.New("route filter references one or more jwt providers that do not exist")
	errRouteFilterNotAcceptedDueToInvalidRefs       = errors.New("route filter is not accepted due to errors with references")
)

func (g authFilterValidationResults) Conditions(generation int64, idx int) []metav1.Condition {
	result := g[idx]
	return result.Conditions(generation)
}

func (g authFilterValidationResult) Conditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		g.acceptedCondition(generation),
		g.resolvedRefsCondition(generation),
	}
}

func (g authFilterValidationResult) acceptedCondition(generation int64) metav1.Condition {
	now := timeFunc()
	if g.acceptedErr != nil {
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "ReferencesNotValid",
			ObservedGeneration: generation,
			Message:            g.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	}
	return metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		ObservedGeneration: generation,
		Message:            "route auth filter accepted",
		LastTransitionTime: now,
	}
}

func (g authFilterValidationResult) resolvedRefsCondition(generation int64) metav1.Condition {
	now := timeFunc()
	if g.resolvedRefErr == nil {
		return metav1.Condition{
			Type:               "ResolvedRefs",
			Status:             metav1.ConditionTrue,
			Reason:             "ResolvedRefs",
			ObservedGeneration: generation,
			Message:            "resolved references",
			LastTransitionTime: now,
		}
	}

	return metav1.Condition{
		Type:               "ResolvedRefs",
		Status:             metav1.ConditionFalse,
		Reason:             "MissingJWTProviderReference",
		ObservedGeneration: generation,
		Message:            g.resolvedRefErr.Error(),
		LastTransitionTime: now,
	}
}
