// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	gwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/custom-gateway-api/apis/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestOCPGatewayPolicyWebhook_Handle(t *testing.T) {
	tests := map[string]struct {
		existingResources []runtime.Object
		newResource       *OCPGatewayPolicy
		expAllow          bool
		expErrMessage     string
		expReason         string
	}{
		"valid - no other policy targets listener": {
			existingResources: []runtime.Object{
				&gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-gateway",
						Namespace: "default",
					},
					Spec: gwv1beta1.GatewaySpec{
						Listeners: []gwv1beta1.Listener{
							{
								Name: "l1",
							},
						},
					},
				},
			},
			newResource: &OCPGatewayPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "default",
				},
				Spec: OCPGatewayPolicySpec{
					TargetRef: OCPPolicyTargetReference{
						Group:       gwv1beta1.GroupVersion.String(),
						Kind:        "Gateway",
						Name:        "my-gateway",
						SectionName: pointerTo(gwv1beta1.SectionName("l1")),
					},
				},
			},
			expAllow: true,
		},
		"valid - existing policy targets different gateway": {
			existingResources: []runtime.Object{
				&gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-gateway",
						Namespace: "default",
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: "",
						Listeners: []gwv1beta1.Listener{
							{
								Name: "l1",
							},
						},
					},
				},
				&OCPGatewayPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-policy-2",
						Namespace: "default",
					},
					Spec: OCPGatewayPolicySpec{
						TargetRef: OCPPolicyTargetReference{
							Group:       gwv1beta1.GroupVersion.String(),
							Kind:        "Gateway",
							Name:        "another-gateway",
							SectionName: pointerTo(gwv1beta1.SectionName("l1")),
						},
					},
				},
			},
			newResource: &OCPGatewayPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "OCPGatewayPolicy",
					Namespace: "default",
				},
				Spec: OCPGatewayPolicySpec{
					TargetRef: OCPPolicyTargetReference{
						Group:       gwv1beta1.GroupVersion.String(),
						Kind:        "Gateway",
						Name:        "my-gateway",
						SectionName: pointerTo(gwv1beta1.SectionName("l1")),
					},
				},
			},
			expAllow: true,
		},

		"valid - existing policy targets different listener on the same gateway": {
			existingResources: []runtime.Object{
				&gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "my-gateway",
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: "",
						Listeners: []gwv1beta1.Listener{
							{
								Name: "l1",
							},
							{
								Name: "l2",
							},
						},
					},
				},
				&OCPGatewayPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-policy-2",
						Namespace: "default",
					},
					Spec: OCPGatewayPolicySpec{
						TargetRef: OCPPolicyTargetReference{
							Group:       gwv1beta1.GroupVersion.String(),
							Kind:        "Gateway",
							Name:        "my-gateway",
							SectionName: pointerTo(gwv1beta1.SectionName("l2")),
						},
					},
				},
			},
			newResource: &OCPGatewayPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy",
					Namespace: "default",
				},
				Spec: OCPGatewayPolicySpec{
					TargetRef: OCPPolicyTargetReference{
						Group:       gwv1beta1.GroupVersion.String(),
						Kind:        "Gateway",
						Name:        "my-gateway",
						SectionName: pointerTo(gwv1beta1.SectionName("l1")),
					},
				},
			},
			expAllow: true,
		},
		"invalid - existing policy targets same listener on same gateway": {
			existingResources: []runtime.Object{
				&gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-gateway",
						Namespace: "default",
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: "",
						Listeners: []gwv1beta1.Listener{
							{
								Name: "l1",
							},
							{
								Name: "l2",
							},
						},
					},
				},
				&OCPGatewayPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-policy",
						Namespace: "default",
					},
					Spec: OCPGatewayPolicySpec{
						TargetRef: OCPPolicyTargetReference{
							Group:       gwv1beta1.GroupVersion.String(),
							Kind:        "Gateway",
							Name:        "my-gateway",
							SectionName: pointerTo(gwv1beta1.SectionName("l1")),
						},
					},
				},
			},
			newResource: &OCPGatewayPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-policy-2",
					Namespace: "default",
				},
				Spec: OCPGatewayPolicySpec{
					TargetRef: OCPPolicyTargetReference{
						Group:       gwv1beta1.GroupVersion.String(),
						Kind:        "Gateway",
						Name:        "my-gateway",
						SectionName: pointerTo(gwv1beta1.SectionName("l1")),
					},
				},
			},
			expAllow:      false,
			expErrMessage: "policy targets gateway listener \"l1\" that is already the target of an existing policy \"my-policy\"",
			expReason:     "Forbidden",
		},
	}
	for name, tt := range tests {
		name := name
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(tt.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &OCPGatewayPolicy{}, &OCPGatewayPolicyList{})
			s.AddKnownTypes(gwv1beta1.SchemeGroupVersion, &gwv1beta1.Gateway{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingResources...).WithIndex(&OCPGatewayPolicy{}, OCPGatewayPolicy_GatewayIndex, gatewayForOCPGatewayPolicy).Build()

			var list OCPGatewayPolicyList

			gwNamespaceName := types.NamespacedName{Name: "my-gateway", Namespace: "default"}
			fakeClient.List(ctx, &list, &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector(OCPGatewayPolicy_GatewayIndex, gwNamespaceName.String()),
			})

			decoder := admission.NewDecoder(s)
			v := &OCPGatewayPolicyWebhook{
				Logger:  logrtest.New(t),
				decoder: decoder,
				Client:  fakeClient,
			}

			response := v.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      tt.newResource.Name,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: marshalledRequestObject,
					},
				},
			})

			assert.Equal(t, tt.expAllow, response.Allowed)
			if tt.expErrMessage != "" {
				require.NotNil(t, response.AdmissionResponse.Result)
				assert.Equal(t, tt.expErrMessage, response.AdmissionResponse.Result.Message)
			}
			if tt.expReason != "" {
				require.NotNil(t, response.AdmissionResponse.Result)
				assert.EqualValues(t, tt.expReason, response.AdmissionResponse.Result.Reason)
			}
		})
	}
}

func pointerTo[T any](v T) *T {
	return &v
}

func gatewayForOCPGatewayPolicy(o client.Object) []string {
	OCPGatewayPolicy := o.(*OCPGatewayPolicy)

	targetGateway := OCPGatewayPolicy.Spec.TargetRef
	// gateway policy is 1to1
	if targetGateway.Group == "gateway.networking.k8s.io/v1beta1" && targetGateway.Kind == "Gateway" {
		policyNamespace := OCPGatewayPolicy.Namespace
		if policyNamespace == "" {
			policyNamespace = "default"
		}
		targetNS := targetGateway.Namespace
		if targetNS == "" {
			targetNS = policyNamespace
		}

		return []string{types.NamespacedName{Name: targetGateway.Name, Namespace: targetNS}.String()}
	}

	return []string{}
}
