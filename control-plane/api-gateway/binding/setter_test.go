// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestSetter(t *testing.T) {
	setter := newSetter("test")
	parentRef := gwv1beta1.ParentReference{
		Name: "test",
	}
	parentRefDup := gwv1beta1.ParentReference{
		Name: "test",
	}
	condition := metav1.Condition{
		Type:    "Accepted",
		Status:  metav1.ConditionTrue,
		Reason:  "Accepted",
		Message: "route accepted",
	}
	route := &gwv1beta1.HTTPRoute{
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{parentRef},
			},
		},
	}
	require.True(t, setter.setRouteCondition(route, &parentRef, condition))
	require.False(t, setter.setRouteCondition(route, &parentRefDup, condition))
	require.False(t, setter.setRouteCondition(route, &parentRefDup, condition))
	require.False(t, setter.setRouteCondition(route, &parentRefDup, condition))

	require.Len(t, route.Status.Parents, 1)
	require.Len(t, route.Status.Parents[0].Conditions, 1)
}
