package binding

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type bindResult struct {
	section gwv1beta1.SectionName
	err     error
}

type bindResults []bindResult

func (b bindResults) Error() string {
	messages := []string{}
	for _, result := range b {
		if result.err != nil {
			messages = append(messages, fmt.Sprintf("%s: %s", result.section, result.err.Error()))
		}
	}

	sort.Strings(messages)
	return strings.Join(messages, "; ")
}

func (b bindResults) DidBind() bool {
	for _, result := range b {
		if result.err == nil {
			return true
		}
	}
	return false
}

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
			// if we have a hostname mismatch error, then use the more specific reason
			if result.err == errNoMatchingListenerHostname {
				reason = "NoMatchingListenerHostname"
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

type parentBindResult struct {
	parent  gwv1beta1.ParentReference
	results bindResults
}

type parentBindResults []parentBindResult
