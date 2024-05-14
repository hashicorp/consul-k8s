// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

// +kubebuilder:object:generate=false

type RegistrationWebhook struct {
	Logger logr.Logger

	// ConsulMeta contains metadata specific to the Consul installation.
	ConsulMeta common.ConsulMeta

	decoder *admission.Decoder
	client.Client
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-v1alpha1-registration,mutating=false,failurePolicy=fail,groups=consul.hashicorp.com,resources=registrations,versions=v1alpha1,name=validate-registration.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *RegistrationWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	resource := &Registration{}
	decodeErr := v.decoder.Decode(req, resource)
	if decodeErr != nil {
		return admission.Errored(http.StatusBadRequest, decodeErr)
	}

	var err error

	err = errors.Join(err, validateRequiredFields(resource))
	err = errors.Join(err, validateHealthChecks(resource))
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.Allowed("registration is valid")
}

func (v *RegistrationWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.Logger.Info("setting up registration webhook")
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/validate-v1alpha1-registration", &admission.Webhook{Handler: v})
}

func validateRequiredFields(registration *Registration) error {
	var err error
	if registration.Spec.Node == "" {
		err = errors.Join(err, errors.New("registration.Spec.Node is required"))
	}
	if registration.Spec.Service.Name == "" {
		err = errors.Join(err, errors.New("registration.Spec.Service.Name is required"))
	}
	if registration.Spec.Address == "" {
		err = errors.Join(err, errors.New("registration.Spec.Address is required"))
	}

	if err != nil {
		return err
	}
	return nil
}

var validStatuses = map[string]struct{}{
	"passing":  {},
	"warning":  {},
	"critical": {},
}

func validateHealthChecks(registration *Registration) error {
	if registration.Spec.HealthCheck == nil {
		return nil
	}

	var err error

	if registration.Spec.HealthCheck.Name == "" {
		err = errors.Join(err, errors.New("registration.Spec.HealthCheck.Name is required"))
	}

	// status must be one "passing", "warning", or "critical"
	if _, ok := validStatuses[registration.Spec.HealthCheck.Status]; !ok {
		err = errors.Join(err, fmt.Errorf("invalid registration.Spec.HealthCheck.Status value, must be 'passing', 'warning', or 'critical', actual: %q", registration.Spec.HealthCheck.Status))
	}

	// parse all durations and check for any errors
	_, parseErr := time.ParseDuration(registration.Spec.HealthCheck.Definition.IntervalDuration)
	if parseErr != nil {
		err = errors.Join(err, fmt.Errorf("invalid registration.Spec.HealthCheck.Definition.IntervalDuration value: %q", registration.Spec.HealthCheck.Definition.IntervalDuration))
	}

	if registration.Spec.HealthCheck.Definition.TimeoutDuration != "" {
		_, timeoutErr := time.ParseDuration(registration.Spec.HealthCheck.Definition.TimeoutDuration)
		if timeoutErr != nil {
			err = errors.Join(err, fmt.Errorf("invalid registration.Spec.HealthCheck.Definition.TimeoutDuration value: %q", registration.Spec.HealthCheck.Definition.TimeoutDuration))
		}
	}

	if registration.Spec.HealthCheck.Definition.DeregisterCriticalServiceAfterDuration != "" {
		_, deregCriticalErr := time.ParseDuration(registration.Spec.HealthCheck.Definition.DeregisterCriticalServiceAfterDuration)
		if deregCriticalErr != nil {
			err = errors.Join(err, fmt.Errorf("invalid registration.Spec.HealthCheck.Definition.DeregisterCriticalServiceAfterDuration value: %q", registration.Spec.HealthCheck.Definition.DeregisterCriticalServiceAfterDuration))
		}
	}

	if err != nil {
		return err
	}

	return nil
}
