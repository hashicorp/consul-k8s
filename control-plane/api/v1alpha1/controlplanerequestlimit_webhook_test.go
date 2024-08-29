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

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

func TestValidateControlPlaneRequestLimit(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *ControlPlaneRequestLimit
		expAllow          bool
		expErrMessage     string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.ControlPlaneRequestLimit,
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: ReadWriteRatesConfig{
						ReadRate:  100,
						WriteRate: 100,
					},
				},
			},
			expAllow: true,
		},
		"invalid resource name": {
			existingResources: nil,
			newResource: &ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid",
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: ReadWriteRatesConfig{
						ReadRate:  100,
						WriteRate: 100,
					},
				},
			},
			expAllow:      false,
			expErrMessage: `controlplanerequestlimit resource name must be "controlplanerequestlimit"`,
		},
		"resource already exists": {
			existingResources: []runtime.Object{
				&ControlPlaneRequestLimit{
					ObjectMeta: metav1.ObjectMeta{
						Name: common.ControlPlaneRequestLimit,
					},
					Spec: ControlPlaneRequestLimitSpec{
						Mode: "permissive",
						ReadWriteRatesConfig: ReadWriteRatesConfig{
							ReadRate:  100,
							WriteRate: 100,
						},
					},
				},
			},
			newResource: &ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.ControlPlaneRequestLimit,
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: ReadWriteRatesConfig{
						ReadRate:  100,
						WriteRate: 100,
					},
				},
			},
			expAllow:      false,
			expErrMessage: `controlplanerequestlimit resource already defined - only one control plane request limit entry is supported`,
		},
		"invalid spec": {
			existingResources: nil,
			newResource: &ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.ControlPlaneRequestLimit,
				},
				Spec: ControlPlaneRequestLimitSpec{
					Mode: "invalid",
					ReadWriteRatesConfig: ReadWriteRatesConfig{
						ReadRate:  100,
						WriteRate: 100,
					},
				},
			},
			expAllow:      false,
			expErrMessage: `controlplanerequestlimit.consul.hashicorp.com "controlplanerequestlimit" is invalid: spec.mode: Invalid value: "invalid": mode must be one of: permissive, enforcing, disabled`,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &ControlPlaneRequestLimit{}, &ControlPlaneRequestLimitList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder := admission.NewDecoder(s)

			validator := &ControlPlaneRequestLimitWebhook{
				Client:  client,
				Logger:  logrtest.New(t),
				decoder: decoder,
			}
			response := validator.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      c.newResource.KubernetesName(),
					Namespace: otherNS,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			})

			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}
