package binding

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type bindResult map[gwv1beta1.SectionName]error

func (b bindResult) Error() string {
	messages := []string{}
	for section, err := range b {
		if err != nil {
			messages = append(messages, fmt.Sprintf("%s: %s", section, err.Error()))
		}
	}

	sort.Strings(messages)
	return strings.Join(messages, "; ")
}

func (b bindResult) DidBind() bool {
	for _, err := range b {
		if err == nil {
			return true
		}
	}
	return false
}

func (b bindResult) Condition() metav1.Condition {
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
		for _, err := range b {
			// if we have a hostname mismatch error, then use the more specific reason
			if err == errNoMatchingListenerHostname {
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
