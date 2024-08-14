// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBindResults_Condition(t *testing.T) {
	testCases := []struct {
		Name     string
		Results  bindResults
		Expected metav1.Condition
	}{
		{
			Name:     "route successfully bound",
			Results:  bindResults{{section: "", err: nil}},
			Expected: metav1.Condition{Type: "Accepted", Status: "True", Reason: "Accepted", Message: "route accepted"},
		},
		{
			Name: "multiple bind results",
			Results: bindResults{
				{section: "abc", err: errRouteNoMatchingListenerHostname},
				{section: "def", err: errRouteNoMatchingParent},
			},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "NotAllowedByListeners", Message: "abc: listener cannot bind route with a non-aligned hostname; def: no matching parent"},
		},
		{
			Name:     "no matching listener hostname error",
			Results:  bindResults{{section: "abc", err: errRouteNoMatchingListenerHostname}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "NoMatchingListenerHostname", Message: "abc: listener cannot bind route with a non-aligned hostname"},
		},
		{
			Name:     "ref not permitted error",
			Results:  bindResults{{section: "abc", err: errRefNotPermitted}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "RefNotPermitted", Message: "abc: reference not permitted due to lack of ReferenceGrant"},
		},
		{
			Name:     "no matching parent error",
			Results:  bindResults{{section: "hello1", err: errRouteNoMatchingParent}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "NoMatchingParent", Message: "hello1: no matching parent"},
		},
		{
			Name:     "bind result without section name",
			Results:  bindResults{{section: "", err: errRouteNoMatchingParent}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "NoMatchingParent", Message: "no matching parent"},
		},
		{
			Name:     "external filter ref not found",
			Results:  bindResults{{section: "", err: errExternalRefNotFound}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "FilterNotFound", Message: "ref not found"},
		},
		{
			Name:     "jwt provider referenced by external filter is not found",
			Results:  bindResults{{section: "", err: errFilterInvalid}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "JWTProviderNotFound", Message: "filter invalid"},
		},
		{
			Name:     "route references invalid filter type",
			Results:  bindResults{{section: "", err: errInvalidExternalRefType}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "UnsupportedValue", Message: "invalid externalref filter kind"},
		},
		{
			Name:     "unhandled error type",
			Results:  bindResults{{section: "abc", err: errors.New("you don't know me")}},
			Expected: metav1.Condition{Type: "Accepted", Status: "False", Reason: "NotAllowedByListeners", Message: "abc: you don't know me"},
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s_%s", t.Name(), tc.Name), func(t *testing.T) {
			actual := tc.Results.Condition()
			assert.Equalf(t, tc.Expected.Type, actual.Type, "expected condition with type %q but got %q", tc.Expected.Type, actual.Type)
			assert.Equalf(t, tc.Expected.Status, actual.Status, "expected condition with status %q but got %q", tc.Expected.Status, actual.Status)
			assert.Equalf(t, tc.Expected.Reason, actual.Reason, "expected condition with reason %q but got %q", tc.Expected.Reason, actual.Reason)
			assert.Equalf(t, tc.Expected.Message, actual.Message, "expected condition with message %q but got %q", tc.Expected.Message, actual.Message)
		})
	}
}

func TestGatewayPolicyValidationResult_Conditions(t *testing.T) {
	t.Parallel()
	var generation int64 = 5
	for name, tc := range map[string]struct {
		results  gatewayPolicyValidationResult
		expected []metav1.Condition
	}{
		"policy valid": {
			results: gatewayPolicyValidationResult{},
			expected: []metav1.Condition{
				{
					Type:               "Accepted",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "Accepted",
					Message:            "gateway policy accepted",
				},
				{
					Type:               "ResolvedRefs",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "ResolvedRefs",
					Message:            "resolved references",
				},
			},
		},
		"errors with JWT references": {
			results: gatewayPolicyValidationResult{
				acceptedErr:      errNotAcceptedDueToInvalidRefs,
				resolvedRefsErrs: []error{errorForMissingJWTProviders(map[string]struct{}{"okta": {}})},
			},
			expected: []metav1.Condition{
				{
					Type:               "Accepted",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "ReferencesNotValid",
					Message:            errNotAcceptedDueToInvalidRefs.Error(),
				},
				{
					Type:               "ResolvedRefs",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "MissingJWTProviderReference",
					Message:            errorForMissingJWTProviders(map[string]struct{}{"okta": {}}).Error(),
				},
			},
		},
		"errors with listener references": {
			results: gatewayPolicyValidationResult{
				acceptedErr:      errNotAcceptedDueToInvalidRefs,
				resolvedRefsErrs: []error{errorForMissingListener("gw", "l1")},
			},
			expected: []metav1.Condition{
				{
					Type:               "Accepted",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "ReferencesNotValid",
					Message:            errNotAcceptedDueToInvalidRefs.Error(),
				},
				{
					Type:               "ResolvedRefs",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "MissingListenerReference",
					Message:            errorForMissingListener("gw", "l1").Error(),
				},
			},
		},
		"errors with listener and jwt references": {
			results: gatewayPolicyValidationResult{
				acceptedErr: errNotAcceptedDueToInvalidRefs,
				resolvedRefsErrs: []error{
					errorForMissingJWTProviders(map[string]struct{}{"okta": {}}),
					errorForMissingListener("gw", "l1"),
				},
			},
			expected: []metav1.Condition{
				{
					Type:               "Accepted",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "ReferencesNotValid",
					Message:            errNotAcceptedDueToInvalidRefs.Error(),
				},
				{
					Type:               "ResolvedRefs",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "MissingJWTProviderReference",
					Message:            errorForMissingJWTProviders(map[string]struct{}{"okta": {}}).Error(),
				},
				{
					Type:               "ResolvedRefs",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "MissingListenerReference",
					Message:            errorForMissingListener("gw", "l1").Error(),
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.EqualValues(t, tc.expected, tc.results.Conditions(generation))
		})
	}
}

func TestAuthFilterValidationResult_Conditions(t *testing.T) {
	t.Parallel()
	var generation int64 = 5
	for name, tc := range map[string]struct {
		results  authFilterValidationResult
		expected []metav1.Condition
	}{
		"policy valid": {
			results: authFilterValidationResult{},
			expected: []metav1.Condition{
				{
					Type:               "Accepted",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "Accepted",
					Message:            "route auth filter accepted",
				},
				{
					Type:               "ResolvedRefs",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "ResolvedRefs",
					Message:            "resolved references",
				},
			},
		},
		"errors with JWT references": {
			results: authFilterValidationResult{
				acceptedErr:    errNotAcceptedDueToInvalidRefs,
				resolvedRefErr: fmt.Errorf("%w: missingProviderNames: %s", errPolicyJWTProvidersReferenceDoesNotExist, "okta"),
			},
			expected: []metav1.Condition{
				{
					Type:               "Accepted",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "ReferencesNotValid",
					Message:            errNotAcceptedDueToInvalidRefs.Error(),
				},
				{
					Type:               "ResolvedRefs",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: generation,
					LastTransitionTime: timeFunc(),
					Reason:             "MissingJWTProviderReference",
					Message:            fmt.Errorf("%w: missingProviderNames: %s", errPolicyJWTProvidersReferenceDoesNotExist, "okta").Error(),
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.EqualValues(t, tc.expected, tc.results.Conditions(generation))
		})
	}
}
