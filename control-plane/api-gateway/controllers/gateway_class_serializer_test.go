package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestSerializeGatewayClassConfig_HappyPath(t *testing.T) {
	t.Parallel()
	type args struct {
		ctx context.Context
		gw  *gwv1beta1.Gateway
		gwc *gwv1beta1.GatewayClass
	}
	tests := []struct {
		name              string
		args              args
		gwcConfig         *v1alpha1.GatewayClassConfig
		expectedDidUpdate bool
		wantErr           bool
	}{
		{
			name: "when gateway has not been annotated yet and annotations are nil",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: true,
			wantErr:           false,
		},
		{
			name: "when gateway has not been annotated yet but annotations are empty",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "my-gw",
						Annotations: make(map[string]string),
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: true,
			wantErr:           false,
		},
		{
			name: "when gateway has been annotated",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
						Annotations: map[string]string{
							annotationConfigKey: `{"serviceType":"serviceType","nodeSelector":{"selector":"of node"},"tolerations":[{"key":"key","operator":"op","value":"120","effect":"to the moon","tolerationSeconds":0}],"copyAnnotations":{"service":["service"]}}`,
						},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))
			objs := []runtime.Object{tt.args.gw, tt.args.gwc, tt.gwcConfig}
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

			actualDidUpdate, err := SerializeGatewayClassConfig(tt.args.ctx, fakeClient, tt.args.gw, tt.args.gwc)
			if (err != nil) != tt.wantErr {
				t.Errorf("SerializeGatewayClassConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if actualDidUpdate != tt.expectedDidUpdate {
				t.Errorf("SerializeGatewayClassConfig() = %v, want %v", actualDidUpdate, tt.expectedDidUpdate)
			}

			var config v1alpha1.GatewayClassConfig
			err = json.Unmarshal([]byte(tt.args.gw.Annotations[annotationConfigKey]), &config.Spec)
			require.NoError(t, err)

			if diff := cmp.Diff(config.Spec, tt.gwcConfig.Spec); diff != "" {
				t.Errorf("Expected gwconfig spec to match serialized version (-want,+got):\n%s", diff)
			}
		})
	}
}

func TestSerializeGatewayClassConfig_SadPath(t *testing.T) {
	t.Parallel()
	type args struct {
		ctx context.Context
		gw  *gwv1beta1.Gateway
		gwc *gwv1beta1.GatewayClass
	}
	tests := []struct {
		name              string
		args              args
		gwcConfig         *v1alpha1.GatewayClassConfig
		expectedDidUpdate bool
		wantErr           bool
	}{
		{
			name: "when gateway has been annotated but the serialization was invalid",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
						Annotations: map[string]string{
							// we remove the opening brace to make unmarshalling fail
							annotationConfigKey: `"serviceType":"serviceType","nodeSelector":{"selector":"of node"},"tolerations":[{"key":"key","operator":"op","value":"120","effect":"to the moon","tolerationSeconds":0}],"copyAnnotations":{"service":["service"]}}`,
						},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           true,
		},
		{
			name: "No Annotation and GatewayConfig is missing the Group field",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "my-gw",
						Annotations: map[string]string{},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: "",
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           false,
		},
		{
			name: "No Annotation and GatewayConfig is missing the Kind field we get an error",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "my-gw",
						Annotations: map[string]string{},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  "",
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           false,
		},
		{
			name: "No Annotation and GatewayConfig is not able to be found",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "my-gw",
						Annotations: map[string]string{},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "NOT GONNA FIND ME",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           false,
		},
		{
			name: "Annotation present and GatewayConfig is missing the Group field",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
						Annotations: map[string]string{
							annotationConfigKey: `{"serviceType":"serviceType","nodeSelector":{"selector":"of node"},"tolerations":[{"key":"key","operator":"op","value":"120","effect":"to the moon","tolerationSeconds":0}],"copyAnnotations":{"service":["service"]}}`,
						},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: "",
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           false,
		},
		{
			name: "Annotation present and GatewayConfig is missing the Kind field we get an error",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
						Annotations: map[string]string{
							annotationConfigKey: `{"serviceType":"serviceType","nodeSelector":{"selector":"of node"},"tolerations":[{"key":"key","operator":"op","value":"120","effect":"to the moon","tolerationSeconds":0}],"copyAnnotations":{"service":["service"]}}`,
						},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  "",
							Name:  "the config",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           false,
		},
		{
			name: "Annotation present and GatewayConfig is not able to be found",
			args: args{
				ctx: context.Background(),
				gw: &gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw",
						Annotations: map[string]string{
							annotationConfigKey: `{"serviceType":"serviceType","nodeSelector":{"selector":"of node"},"tolerations":[{"key":"key","operator":"op","value":"120","effect":"to the moon","tolerationSeconds":0}],"copyAnnotations":{"service":["service"]}}`,
						},
					},
					Spec:   gwv1beta1.GatewaySpec{},
					Status: gwv1beta1.GatewayStatus{},
				},
				gwc: &gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-gw-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: "",
						ParametersRef: &gwv1beta1.ParametersReference{
							Group: Group,
							Kind:  v1alpha1.GatewayClassConfigKind,
							Name:  "NOT GONNA FIND ME",
						},
						Description: new(string),
					},
				},
			},
			gwcConfig: &v1alpha1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: "the config",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					ServiceType: pointerTo(corev1.ServiceType("serviceType")),
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
			expectedDidUpdate: false,
			wantErr:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))
			objs := []runtime.Object{tt.args.gw, tt.args.gwc, tt.gwcConfig}
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

			actualDidUpdate, err := SerializeGatewayClassConfig(tt.args.ctx, fakeClient, tt.args.gw, tt.args.gwc)
			if (err != nil) != tt.wantErr {
				t.Errorf("SerializeGatewayClassConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if actualDidUpdate != tt.expectedDidUpdate {
				t.Errorf("SerializeGatewayClassConfig() = %v, want %v", actualDidUpdate, tt.expectedDidUpdate)
			}

			// don't bother checking the annotation if there was an error
			if err != nil {
				return
			}

			fmt.Printf("%p\n", tt.args.gw)

			require.Empty(t, tt.args.gw.Annotations[annotationConfigKey])
		})
	}
}
