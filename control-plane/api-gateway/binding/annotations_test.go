// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestSerializeGatewayClassConfig_HappyPath(t *testing.T) {
	t.Parallel()

	type args struct {
		gw   *gwv1beta1.Gateway
		gwcc *v1alpha1.GatewayClassConfig
	}
	tests := []struct {
		name              string
		args              args
		expectedDidUpdate bool
	}{
		{
			name: "when gateway has not been annotated yet and annotations are nil",
			args: args{
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwcc: &v1alpha1.GatewayClassConfig{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "the config",
					},
					Spec: v1alpha1.GatewayClassConfigSpec{
						ServiceType: common.PointerTo(corev1.ServiceType("serviceType")),
						NodeSelector: map[string]string{
							"selector": "of node",
						},
						Tolerations: []v1.Toleration{
							{
								Key:               "key",
								Operator:          "op",
								Value:             "120",
								Effect:            "to the moon",
								TolerationSeconds: new(int64),
							},
						},
						CopyAnnotations: v1alpha1.CopyAnnotationsSpec{
							Service: []string{"service"},
						},
					},
				},
			},
			expectedDidUpdate: true,
		},
		{
			name: "when gateway has not been annotated yet but annotations are empty",
			args: args{
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "my-gw",
						Annotations: make(map[string]string),
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwcc: &v1alpha1.GatewayClassConfig{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "the config",
					},
					Spec: v1alpha1.GatewayClassConfigSpec{
						ServiceType: common.PointerTo(corev1.ServiceType("serviceType")),
						NodeSelector: map[string]string{
							"selector": "of node",
						},
						Tolerations: []v1.Toleration{
							{
								Key:               "key",
								Operator:          "op",
								Value:             "120",
								Effect:            "to the moon",
								TolerationSeconds: new(int64),
							},
						},
						CopyAnnotations: v1alpha1.CopyAnnotationsSpec{
							Service: []string{"service"},
						},
					},
				},
			},
			expectedDidUpdate: true,
		},
		{
			name: "when gateway has been annotated",
			args: args{
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
						Annotations: map[string]string{
							common.AnnotationGatewayClassConfig: `{"serviceType":"serviceType","nodeSelector":{"selector":"of node"},"tolerations":[{"key":"key","operator":"op","value":"120","effect":"to the moon","tolerationSeconds":0}],"copyAnnotations":{"service":["service"]}}`,
						},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwcc: &v1alpha1.GatewayClassConfig{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "the config",
					},
					Spec: v1alpha1.GatewayClassConfigSpec{
						ServiceType: common.PointerTo(corev1.ServiceType("serviceType")),
						NodeSelector: map[string]string{
							"selector": "of node",
						},
						Tolerations: []v1.Toleration{
							{
								Key:               "key",
								Operator:          "op",
								Value:             "120",
								Effect:            "to the moon",
								TolerationSeconds: new(int64),
							},
						},
						CopyAnnotations: v1alpha1.CopyAnnotationsSpec{
							Service: []string{"service"},
						},
					},
				},
			},
			expectedDidUpdate: false,
		},
		{
			name: "when gateway has been annotated but the serialization was invalid",
			args: args{
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
						Annotations: map[string]string{
							// we remove the opening brace to make unmarshalling fail
							common.AnnotationGatewayClassConfig: `"serviceType":"serviceType","nodeSelector":{"selector":"of node"},"tolerations":[{"key":"key","operator":"op","value":"120","effect":"to the moon","tolerationSeconds":0}],"copyAnnotations":{"service":["service"]}}`,
						},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwcc: &v1alpha1.GatewayClassConfig{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "the config",
					},
					Spec: v1alpha1.GatewayClassConfigSpec{
						ServiceType: common.PointerTo(corev1.ServiceType("serviceType")),
						NodeSelector: map[string]string{
							"selector": "of node",
						},
						Tolerations: []v1.Toleration{
							{
								Key:               "key",
								Operator:          "op",
								Value:             "120",
								Effect:            "to the moon",
								TolerationSeconds: new(int64),
							},
						},
						CopyAnnotations: v1alpha1.CopyAnnotationsSpec{
							Service: []string{"service"},
						},
					},
				},
			},
			expectedDidUpdate: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, actualDidUpdate := serializeGatewayClassConfig(tt.args.gw, tt.args.gwcc)

			if actualDidUpdate != tt.expectedDidUpdate {
				t.Errorf("SerializeGatewayClassConfig() = %v, want %v", actualDidUpdate, tt.expectedDidUpdate)
			}

			var config v1alpha1.GatewayClassConfig
			err := json.Unmarshal([]byte(tt.args.gw.Annotations[common.AnnotationGatewayClassConfig]), &config.Spec)
			require.NoError(t, err)

			if diff := cmp.Diff(config.Spec, tt.args.gwcc.Spec); diff != "" {
				t.Errorf("Expected gwconfig spec to match serialized version (-want,+got):\n%s", diff)
			}
		})
	}
}
