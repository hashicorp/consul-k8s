package registration

import (
	"errors"

	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// Conditions
const (
	ConditionSynced      = "Synced"
	ConditionRegistered  = "Registered"
	ConditionACLsUpdated = "ACLsUpdated"
)

// Status Reasons.
const (
	SyncError                 = "SyncError"
	ConsulErrorRegistration   = "ConsulErrorRegistration"
	ConsulErrorDeregistration = "ConsulErrorDeregistration"
	ConsulErrorACL            = "ConsulErrorACL"
	ConsulDerigistration      = "ConsulDeregistration"
)

type Result struct {
	Sync         error
	Registration error
	ACLUpdate    error
	Finalizer    error
}

func (r Result) hasErrors() bool {
	return r.Sync != nil || r.Registration != nil || r.ACLUpdate != nil || r.Finalizer != nil
}

func (r Result) errors() error {
	var err error
	err = errors.Join(err, r.Sync, r.Registration, r.ACLUpdate, r.Finalizer)
	return err
}

func syncedCondition(result Result) v1alpha1.Condition {
	if result.Sync != nil {
		return v1alpha1.Condition{
			Type:    ConditionSynced,
			Status:  corev1.ConditionFalse,
			Reason:  SyncError,
			Message: result.Sync.Error(),
		}
	}
	return v1alpha1.Condition{
		Type:   ConditionSynced,
		Status: corev1.ConditionTrue,
	}
}

func registrationCondition(result Result) v1alpha1.Condition {
	if result.Registration != nil {
		reason := ConsulErrorRegistration
		if errors.Is(result.Sync, ErrDeregisteringService) {
			reason = ConsulErrorDeregistration
		}

		return v1alpha1.Condition{
			Type:    ConditionRegistered,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: result.Registration.Error(),
		}
	}
	return v1alpha1.Condition{
		Type:   ConditionRegistered,
		Status: corev1.ConditionTrue,
	}
}

func aclCondition(result Result) v1alpha1.Condition {
	if result.ACLUpdate != nil {
		return v1alpha1.Condition{
			Type:    ConditionACLsUpdated,
			Status:  corev1.ConditionFalse,
			Reason:  ConsulErrorACL,
			Message: result.ACLUpdate.Error(),
		}
	}
	return v1alpha1.Condition{
		Type:   ConditionACLsUpdated,
		Status: corev1.ConditionTrue,
	}
}
