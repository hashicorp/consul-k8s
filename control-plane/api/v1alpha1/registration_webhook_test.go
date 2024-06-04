// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidateRegistration(t *testing.T) {
	cases := map[string]struct {
		newResource        *Registration
		expectedToAllow    bool
		expectedErrMessage string
	}{
		"valid with health check, status 'passing'": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "node-virtual",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "passing",
						Definition: HealthCheckDefinition{
							IntervalDuration: "10s",
						},
					},
				},
			},
			expectedToAllow: true,
		},
		"valid with health check, status 'warning'": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "node-virtual",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "warning",
						Definition: HealthCheckDefinition{
							IntervalDuration: "10s",
						},
					},
				},
			},
			expectedToAllow: true,
		},
		"valid with health check, status 'critical'": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "node-virtual",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "critical",
						Definition: HealthCheckDefinition{
							IntervalDuration: "10s",
						},
					},
				},
			},
			expectedToAllow: true,
		},
		"valid without health check": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:        "node-virtual",
					Address:     "10.2.2.1",
					Service:     Service{Name: "test-service"},
					HealthCheck: nil,
				},
			},
			expectedToAllow: true,
		},
		"invalid, missing node field": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:        "",
					Address:     "10.2.2.1",
					Service:     Service{Name: "test-service"},
					HealthCheck: nil,
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "registration.Spec.Node is required",
		},
		"invalid, missing address field": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:        "test-node",
					Address:     "",
					Service:     Service{Name: "test-service"},
					HealthCheck: nil,
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "registration.Spec.Address is required",
		},
		"invalid, missing service.name field": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:        "test-node",
					Address:     "10.2.2.1",
					Service:     Service{Name: ""},
					HealthCheck: nil,
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "registration.Spec.Service.Name is required",
		},
		"invalid, health check is set and name is missing": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "test-node",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "",
						Status: "passing",
						Definition: HealthCheckDefinition{
							IntervalDuration: "10s",
						},
					},
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "registration.Spec.HealthCheck.Name is required",
		},
		"invalid, health check is set and intervalDuration is missing": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "test-node",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "passing",
						Definition: HealthCheckDefinition{
							IntervalDuration: "",
						},
					},
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "invalid registration.Spec.HealthCheck.Definition.IntervalDuration value: \"\"",
		},
		"invalid, health check is set and intervalDuration is invalid duration type": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "test-node",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "passing",
						Definition: HealthCheckDefinition{
							IntervalDuration: "150",
						},
					},
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "invalid registration.Spec.HealthCheck.Definition.IntervalDuration value: \"150\"",
		},
		"invalid, health check is set and timeoutDuration is invalid duration type": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "test-node",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "passing",
						Definition: HealthCheckDefinition{
							IntervalDuration: "10s",
							TimeoutDuration:  "150",
						},
					},
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "invalid registration.Spec.HealthCheck.Definition.TimeoutDuration value: \"150\"",
		},
		"invalid, health check is set and deregisterCriticalServiceAfterDuration is invalid duration type": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "test-node",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "passing",
						Definition: HealthCheckDefinition{
							IntervalDuration:                       "10s",
							TimeoutDuration:                        "150s",
							DeregisterCriticalServiceAfterDuration: "40",
						},
					},
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "invalid registration.Spec.HealthCheck.Definition.DeregisterCriticalServiceAfterDuration value: \"40\"",
		},
		"invalid, health check is set and status is not 'passing', 'critical', or 'warning'": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "test-node",
					Address: "10.2.2.1",
					Service: Service{Name: "test-service"},
					HealthCheck: &HealthCheck{
						Name:   "check name",
						Status: "wrong",
						Definition: HealthCheckDefinition{
							IntervalDuration: "10s",
						},
					},
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "invalid registration.Spec.HealthCheck.Status value, must be 'passing', 'warning', or 'critical', actual: \"wrong\"",
		},
		"everything that can go wrong has gone wrong": {
			newResource: &Registration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-registration",
				},
				Spec: RegistrationSpec{
					Node:    "",
					Address: "",
					Service: Service{Name: ""},
					HealthCheck: &HealthCheck{
						Name:   "",
						Status: "wrong",
						Definition: HealthCheckDefinition{
							IntervalDuration:                       "10",
							TimeoutDuration:                        "150",
							DeregisterCriticalServiceAfterDuration: "40",
						},
					},
				},
			},
			expectedToAllow:    false,
			expectedErrMessage: "registration.Spec.Node is required\nregistration.Spec.Service.Name is required\nregistration.Spec.Address is required\nregistration.Spec.HealthCheck.Name is required\ninvalid registration.Spec.HealthCheck.Status value, must be 'passing', 'warning', or 'critical', actual: \"wrong\"\ninvalid registration.Spec.HealthCheck.Definition.IntervalDuration value: \"10\"\ninvalid registration.Spec.HealthCheck.Definition.TimeoutDuration value: \"150\"\ninvalid registration.Spec.HealthCheck.Definition.DeregisterCriticalServiceAfterDuration value: \"40\"",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &Registration{}, &RegistrationList{})
			client := fake.NewClientBuilder().WithScheme(s).Build()
			decoder := admission.NewDecoder(s)

			validator := &RegistrationWebhook{
				Client:  client,
				Logger:  logrtest.New(t),
				decoder: decoder,
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			})

			require.Equal(t, c.expectedToAllow, response.Allowed)
			if c.expectedErrMessage != "" {
				require.Equal(t, c.expectedErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}
