// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidateProxyDefault(t *testing.T) {
	otherNS := "other"

	cases := map[string]struct {
		existingResources []runtime.Object
		newResource       *ProxyDefaults
		expAllow          bool
		expErrMessage     string
	}{
		"no duplicates, valid": {
			existingResources: nil,
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{},
			},
			expAllow: true,
		},
		"invalid config": {
			existingResources: nil,
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage("1"),
				},
			},
			expAllow: false,
			// This error message is because the value "1" is valid JSON but is an invalid map
			expErrMessage: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.config: Invalid value: \"1\": must be valid map value: json: cannot unmarshal number into Go value of type map[string]interface {}",
		},
		"proxy default exists": {
			existingResources: []runtime.Object{&ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
			}},
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{
					MeshGateway: MeshGateway{
						Mode: "local",
					},
				},
			},
			expAllow:      false,
			expErrMessage: "proxydefaults resource already defined - only one global entry is supported",
		},
		"name not global": {
			existingResources: []runtime.Object{},
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "local",
				},
			},
			expAllow:      false,
			expErrMessage: "proxydefaults resource name must be \"global\"",
		},
		"transparentProxy.outboundListenerPort set": {
			existingResources: []runtime.Object{},
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
					},
				},
			},
			expAllow:      false,
			expErrMessage: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.transparentProxy.outboundListenerPort: Invalid value: 1000: use the annotation `consul.hashicorp.com/transparent-proxy-outbound-listener-port` to configure the Outbound Listener Port",
		},
		"mode value set": {
			existingResources: []runtime.Object{},
			newResource: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					Mode: proxyModeRef("transparent"),
				},
			},
			expAllow:      false,
			expErrMessage: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.mode: Invalid value: \"transparent\": use the annotation `consul.hashicorp.com/transparent-proxy` to configure the Transparent Proxy Mode",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			marshalledRequestObject, err := json.Marshal(c.newResource)
			require.NoError(t, err)
			s := runtime.NewScheme()
			s.AddKnownTypes(GroupVersion, &ProxyDefaults{}, &ProxyDefaultsList{})
			client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.existingResources...).Build()
			decoder := admission.NewDecoder(s)

			validator := &ProxyDefaultsWebhook{
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
