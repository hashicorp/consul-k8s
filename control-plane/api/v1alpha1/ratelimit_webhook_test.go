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

func TestValidateRateLimit(t *testing.T) {
	readRate := func(v float64) *float64 {
		return &v
	}
	writeRate := func(v float64) *float64 {
		return &v
	}

	cases := map[string]struct {
		existingResources       []runtime.Object
		newResource             *RateLimit
		enableACLs              bool
		enablePartitions        bool
		hasGlobalConfigACLToken bool
		expAllow                bool
		expErrMessage           string
	}{
		"no duplicates, valid": {
			existingResources:       nil,
			enablePartitions:        false,
			hasGlobalConfigACLToken: false,
			newResource: &RateLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: RateLimitSpec{
					Config: GlobalRateLimitConfig{
						ReadRate:         readRate(100),
						WriteRate:        writeRate(50),
						ExcludeEndpoints: []string{"Health.Check"},
					},
				},
			},
			expAllow: true,
		},
		"invalid resource name": {
			existingResources:       nil,
			enablePartitions:        false,
			hasGlobalConfigACLToken: false,
			newResource: &RateLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "local",
				},
				Spec: RateLimitSpec{
					Config: GlobalRateLimitConfig{},
				},
			},
			expAllow:      false,
			expErrMessage: `ratelimit resource name must be "global"`,
		},
		"resource already exists": {
			existingResources: []runtime.Object{
				&RateLimit{
					ObjectMeta: metav1.ObjectMeta{
						Name: "global",
					},
					Spec: RateLimitSpec{
						Config: GlobalRateLimitConfig{},
					},
				},
			},
			newResource: &RateLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: RateLimitSpec{
					Config: GlobalRateLimitConfig{},
				},
			},
			enablePartitions:        false,
			hasGlobalConfigACLToken: false,
			expAllow:                false,
			expErrMessage:           "ratelimit resource already defined - only one rate limit entry is supported",
		},
		"invalid config": {
			existingResources:       nil,
			enablePartitions:        false,
			hasGlobalConfigACLToken: false,
			newResource: &RateLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: RateLimitSpec{
					Config: GlobalRateLimitConfig{
						ReadRate: readRate(-1),
					},
				},
			},
			expAllow:      false,
			expErrMessage: `spec.config.readRate: Invalid value: -1: readRate must be non-negative`,
		},
		"admin partitions enabled without global config ACL token": {
			existingResources:       nil,
			enableACLs:              true,
			enablePartitions:        true,
			hasGlobalConfigACLToken: false,
			newResource: &RateLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: RateLimitSpec{
					Config: GlobalRateLimitConfig{},
				},
			},
			expAllow:      false,
			expErrMessage: "connectInject.globalConfigACLToken must be configured when admin partitions are enabled before creating or updating RateLimit resources",
		},
		"admin partitions enabled with global config ACL token": {
			existingResources:       nil,
			enableACLs:              true,
			enablePartitions:        true,
			hasGlobalConfigACLToken: true,
			newResource: &RateLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: RateLimitSpec{
					Config: GlobalRateLimitConfig{},
				},
			},
			expAllow: true,
		},
		"admin partitions enabled without ACLs": {
			existingResources:       nil,
			enableACLs:              false,
			enablePartitions:        true,
			hasGlobalConfigACLToken: false,
			newResource: &RateLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: RateLimitSpec{
					Config: GlobalRateLimitConfig{},
				},
			},
			expAllow: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)

			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &RateLimit{}, &RateLimitList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder := admission.NewDecoder(s)

			validator := &RateLimitWebhook{
				Client:                  client,
				Logger:                  logrtest.New(t),
				decoder:                 decoder,
				EnableACLs:              c.enableACLs,
				EnablePartitions:        c.enablePartitions,
				HasGlobalConfigACLToken: c.hasGlobalConfigACLToken,
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

			require.Equal(t, c.expAllow, response.Allowed)
			if c.expErrMessage != "" {
				require.Equal(t, c.expErrMessage, response.AdmissionResponse.Result.Message)
			}
		})
	}
}
