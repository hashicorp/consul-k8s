// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package registration

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// Conditions.
const (
	ConditionSynced       = "Synced"
	ConditionRegistered   = "Registered"
	ConditionDeregistered = "Deregistered"
)

// Status Reasons.
const (
	SyncError                 = "SyncError"
	ConsulErrorRegistration   = "ConsulErrorRegistration"
	ConsulErrorDeregistration = "ConsulErrorDeregistration"
	ConsulDeregistration      = "ConsulDeregistration"
)

type Result struct {
	Registering        bool
	ConsulDeregistered bool
	Sync               error
	Registration       error
	Deregistration     error
	Finalizer          error
}

func (r Result) hasErrors() bool {
	return r.Sync != nil || r.Registration != nil || r.Finalizer != nil
}

func (r Result) errors() error {
	var err error
	err = errors.Join(err, r.Sync, r.Registration, r.Finalizer)
	return err
}

func syncedCondition(result Result) v1alpha1.Condition {
	if result.Sync != nil {
		return v1alpha1.Condition{
			Type:               ConditionSynced,
			Status:             corev1.ConditionFalse,
			Reason:             SyncError,
			Message:            result.Sync.Error(),
			LastTransitionTime: metav1.Now(),
		}
	}
	return v1alpha1.Condition{
		Type:               ConditionSynced,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	}
}

func registrationCondition(result Result) v1alpha1.Condition {
	if result.Registration != nil {
		return v1alpha1.Condition{
			Type:               ConditionRegistered,
			Status:             corev1.ConditionFalse,
			Reason:             ConsulErrorRegistration,
			Message:            result.Registration.Error(),
			LastTransitionTime: metav1.Now(),
		}
	}
	return v1alpha1.Condition{
		Type:               ConditionRegistered,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	}
}

func deregistrationCondition(result Result) v1alpha1.Condition {
	if result.Deregistration != nil {
		return v1alpha1.Condition{
			Type:               ConditionDeregistered,
			Status:             corev1.ConditionFalse,
			Reason:             ConsulErrorDeregistration,
			Message:            result.Deregistration.Error(),
			LastTransitionTime: metav1.Now(),
		}
	}

	var (
		reason  string
		message string
	)
	if result.ConsulDeregistered {
		reason = ConsulDeregistration
		message = "Consul deregistered this service"
	}
	return v1alpha1.Condition{
		Type:               ConditionDeregistered,
		Status:             corev1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}
