// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// newScalingTestScheme returns a scheme with all types required for scaling tests.
func newScalingTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, gwv1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	require.NoError(t, autoscalingv2.AddToScheme(s))
	return s
}

// makeGateway builds a minimal Gateway with optional annotations for test use.
func makeGateway(name, namespace string, annotations map[string]string) gwv1.Gateway {
	return gwv1.Gateway{
		TypeMeta: metav1.TypeMeta{APIVersion: "gateway.networking.k8s.io/v1", Kind: "Gateway"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: gwv1.GatewaySpec{
			Listeners: []gwv1.Listener{
				{Name: "http", Port: 8080, Protocol: gwv1.HTTPProtocolType},
			},
		},
	}
}

// makeUserHPA builds an HPA that is NOT owned by a Gateway (simulates a user-managed HPA).
func makeUserHPA(name, namespace, targetDeployment string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "autoscaling/v2",
			Kind:       "HorizontalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			// No OwnerReferences → not controller-managed
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       targetDeployment,
			},
			MaxReplicas: 10,
		},
	}
}

// TestDetermineScalingMode validates the priority chain:
//
//	user-managed HPA  >  Gateway annotations  >  GCC deployment spec  >  none
func TestDetermineScalingMode(t *testing.T) {
	t.Parallel()

	const (
		gwName    = "my-gateway"
		gwNS      = "default"
		hpaName   = "user-hpa"
		deployKey = gwName // gateway and its deployment share the same name
	)

	tests := []struct {
		name          string
		annotations   map[string]string
		gccDeploySpec v1alpha1.DeploymentSpec
		existingHPAs  []*autoscalingv2.HorizontalPodAutoscaler
		// assertions
		wantMode        string
		wantStaticValue *int32 // non-nil when mode == "static"
		wantHPAMin      *int32 // non-nil when mode == "hpa-controller"
		wantHPAMax      *int32
		wantError       bool
	}{
		{
			name: "user-managed HPA has highest priority, overrides annotations",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "5",
			},
			existingHPAs: []*autoscalingv2.HorizontalPodAutoscaler{
				makeUserHPA(hpaName, gwNS, deployKey),
			},
			wantMode: "hpa-user",
		},
		{
			name: "user-managed HPA has highest priority, overrides GCC",
			gccDeploySpec: v1alpha1.DeploymentSpec{
				DefaultInstances: int32Ptr(3),
				MaxInstances:     int32Ptr(5),
			},
			existingHPAs: []*autoscalingv2.HorizontalPodAutoscaler{
				makeUserHPA(hpaName, gwNS, deployKey),
			},
			wantMode: "hpa-user",
		},
		{
			name: "static annotation takes priority over GCC deployment spec",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "12",
			},
			gccDeploySpec: v1alpha1.DeploymentSpec{
				DefaultInstances: int32Ptr(3),
				MaxInstances:     int32Ptr(8),
			},
			wantMode:        "static",
			wantStaticValue: int32Ptr(12),
		},
		{
			name: "static annotation allows replicas beyond the old hard limit of 8",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "20",
			},
			wantMode:        "static",
			wantStaticValue: int32Ptr(20),
		},
		{
			name: "HPA annotation takes priority over GCC deployment spec",
			annotations: map[string]string{
				AnnotationHPAEnabled:     "true",
				AnnotationHPAMinReplicas: "4",
				AnnotationHPAMaxReplicas: "30",
				AnnotationHPACPUTarget:   "75",
			},
			gccDeploySpec: v1alpha1.DeploymentSpec{
				DefaultInstances: int32Ptr(3),
				MaxInstances:     int32Ptr(8),
			},
			wantMode:   "hpa-controller",
			wantHPAMin: int32Ptr(4),
			wantHPAMax: int32Ptr(30),
		},
		{
			name: "GCC deployment spec is fallback when no annotations are present",
			gccDeploySpec: v1alpha1.DeploymentSpec{
				DefaultInstances: int32Ptr(3),
				MaxInstances:     int32Ptr(5),
			},
			wantMode:        "static",
			wantStaticValue: int32Ptr(3),
		},
		{
			name:          "no annotations and no GCC spec returns none",
			gccDeploySpec: v1alpha1.DeploymentSpec{},
			wantMode:      "none",
		},
		{
			name: "invalid static replicas annotation returns error",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "not-a-number",
			},
			wantError: true,
		},
		{
			name: "invalid HPA annotations (min > max) returns error",
			annotations: map[string]string{
				AnnotationHPAEnabled:     "true",
				AnnotationHPAMinReplicas: "15",
				AnnotationHPAMaxReplicas: "5",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newScalingTestScheme(t)
			gw := makeGateway(gwName, gwNS, tt.annotations)

			objs := []client.Object{&gw}
			for _, hpa := range tt.existingHPAs {
				objs = append(objs, hpa)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
			log := logrtest.New(t)
			g := New(log, fakeClient, nil, fakeClient)

			gcc := v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gcc"},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: tt.gccDeploySpec,
				},
			}

			result, err := g.DetermineScalingMode(context.Background(), gw, gcc)

			if tt.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantMode, result.Mode)

			if tt.wantStaticValue != nil {
				require.NotNil(t, result.StaticReplicas)
				require.Equal(t, *tt.wantStaticValue, *result.StaticReplicas)
			}

			if tt.wantHPAMin != nil {
				require.NotNil(t, result.HPAConfig)
				require.Equal(t, *tt.wantHPAMin, result.HPAConfig.MinReplicas)
			}

			if tt.wantHPAMax != nil {
				require.NotNil(t, result.HPAConfig)
				require.Equal(t, *tt.wantHPAMax, result.HPAConfig.MaxReplicas)
			}
		})
	}
}

// TestReconcileScaling validates that ReconcileScaling applies the correct HPA side-effects:
//   - hpa-controller mode creates/updates a controller-managed HPA
//   - static mode deletes any controller-managed HPA
//   - none mode deletes any controller-managed HPA
//   - hpa-user mode leaves user HPA untouched and deletes controller HPA
func TestReconcileScaling(t *testing.T) {
	t.Parallel()

	const (
		gwName = "reconcile-gw"
		gwNS   = "default"
	)

	hpaName := fmt.Sprintf("%s-hpa", gwName)

	// controllerOwnedHPA simulates an HPA previously created by the controller.
	controllerOwnedHPA := func(gw *gwv1.Gateway) *autoscalingv2.HorizontalPodAutoscaler {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "autoscaling/v2",
				Kind:       "HorizontalPodAutoscaler",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      hpaName,
				Namespace: gwNS,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "gateway.networking.k8s.io/v1",
						Kind:       "Gateway",
						Name:       gw.Name,
						UID:        gw.UID,
						Controller: func() *bool { b := true; return &b }(),
					},
				},
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       gwName,
				},
				MaxReplicas: 5,
			},
		}
		return hpa
	}

	tests := []struct {
		name           string
		annotations    map[string]string
		gccDeploySpec  v1alpha1.DeploymentSpec
		existingHPAs   func(gw *gwv1.Gateway) []*autoscalingv2.HorizontalPodAutoscaler
		wantMode       string
		wantHPACreated bool // controller-managed HPA should exist after reconcile
		wantHPAAbsent  bool // no controller-managed HPA should exist after reconcile
	}{
		{
			name: "hpa-controller mode creates controller-managed HPA",
			annotations: map[string]string{
				AnnotationHPAEnabled:     "true",
				AnnotationHPAMinReplicas: "3",
				AnnotationHPAMaxReplicas: "25",
				AnnotationHPACPUTarget:   "70",
			},
			existingHPAs:   nil,
			wantMode:       "hpa-controller",
			wantHPACreated: true,
		},
		{
			name: "hpa-controller mode updates existing controller-managed HPA",
			annotations: map[string]string{
				AnnotationHPAEnabled:     "true",
				AnnotationHPAMinReplicas: "5",
				AnnotationHPAMaxReplicas: "50",
				AnnotationHPACPUTarget:   "60",
			},
			existingHPAs: func(gw *gwv1.Gateway) []*autoscalingv2.HorizontalPodAutoscaler {
				return []*autoscalingv2.HorizontalPodAutoscaler{controllerOwnedHPA(gw)}
			},
			wantMode:       "hpa-controller",
			wantHPACreated: true,
		},
		{
			name: "static mode deletes pre-existing controller-managed HPA",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "10",
			},
			existingHPAs: func(gw *gwv1.Gateway) []*autoscalingv2.HorizontalPodAutoscaler {
				return []*autoscalingv2.HorizontalPodAutoscaler{controllerOwnedHPA(gw)}
			},
			wantMode:      "static",
			wantHPAAbsent: true,
		},
		{
			name:          "none mode deletes pre-existing controller-managed HPA",
			annotations:   nil,
			gccDeploySpec: v1alpha1.DeploymentSpec{},
			existingHPAs: func(gw *gwv1.Gateway) []*autoscalingv2.HorizontalPodAutoscaler {
				return []*autoscalingv2.HorizontalPodAutoscaler{controllerOwnedHPA(gw)}
			},
			wantMode:      "none",
			wantHPAAbsent: true,
		},
		{
			name:        "hpa-user mode does not delete user-managed HPA",
			annotations: nil,
			existingHPAs: func(gw *gwv1.Gateway) []*autoscalingv2.HorizontalPodAutoscaler {
				// A user-managed HPA targeting the same deployment (no owner reference)
				return []*autoscalingv2.HorizontalPodAutoscaler{makeUserHPA("user-hpa", gwNS, gwName)}
			},
			wantMode: "hpa-user",
			// The controller-managed HPA (<gwName>-hpa) should not exist; user's HPA is untouched.
			wantHPAAbsent: true, // specifically the <gwName>-hpa; user's "user-hpa" remains
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newScalingTestScheme(t)
			gw := makeGateway(gwName, gwNS, tt.annotations)

			objs := []client.Object{&gw}
			if tt.existingHPAs != nil {
				for _, hpa := range tt.existingHPAs(&gw) {
					objs = append(objs, hpa)
				}
			}

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
			log := logrtest.New(t)
			g := New(log, fakeClient, nil, fakeClient)

			gcc := v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gcc"},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: tt.gccDeploySpec,
				},
			}

			result, err := g.ReconcileScaling(context.Background(), gw, gcc)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantMode, result.Mode)

			// Verify controller-managed HPA state.
			hpa := &autoscalingv2.HorizontalPodAutoscaler{}
			getErr := fakeClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: gwNS}, hpa)

			if tt.wantHPACreated {
				require.NoError(t, getErr, "expected controller-managed HPA %s/%s to exist", gwNS, hpaName)
			}

			if tt.wantHPAAbsent {
				require.True(t, client.IgnoreNotFound(getErr) == nil && getErr != nil,
					"expected controller-managed HPA %s/%s to be absent, but got: %v", gwNS, hpaName, getErr)
			}
		})
	}
}
