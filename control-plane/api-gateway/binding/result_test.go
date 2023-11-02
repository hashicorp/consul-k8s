// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
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
