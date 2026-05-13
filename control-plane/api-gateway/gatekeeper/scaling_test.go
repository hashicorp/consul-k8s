// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestParseScalingAnnotations(t *testing.T) {
	log := logr.Discard()

	tests := []struct {
		name        string
		annotations map[string]string
		expected    *ScalingConfig
		expectError bool
	}{
		{
			name:        "no annotations",
			annotations: nil,
			expected:    &ScalingConfig{Mode: "none"},
			expectError: false,
		},
		{
			name: "static replicas valid",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "15",
			},
			expected: &ScalingConfig{
				Mode:           "static",
				StaticReplicas: int32Ptr(15),
			},
			expectError: false,
		},
		{
			name: "static replicas invalid - not a number",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "invalid",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "static replicas invalid - zero",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "0",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "static replicas invalid - negative",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "-5",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "HPA enabled with defaults",
			annotations: map[string]string{
				AnnotationHPAEnabled: "true",
			},
			expected: &ScalingConfig{
				Mode: "hpa-controller",
				HPAConfig: &HPAConfig{
					MinReplicas:    defaultHPAMinReplicas,
					MaxReplicas:    defaultHPAMaxReplicas,
					CPUTargetValue: defaultCPUTarget,
				},
			},
			expectError: false,
		},
		{
			name: "HPA enabled with custom values",
			annotations: map[string]string{
				AnnotationHPAEnabled:     "true",
				AnnotationHPAMinReplicas: "2",
				AnnotationHPAMaxReplicas: "50",
				AnnotationHPACPUTarget:   "70",
			},
			expected: &ScalingConfig{
				Mode: "hpa-controller",
				HPAConfig: &HPAConfig{
					MinReplicas:    2,
					MaxReplicas:    50,
					CPUTargetValue: 70,
				},
			},
			expectError: false,
		},
		{
			name: "legacy hyphenated CPU target spelling remains supported",
			annotations: map[string]string{
				AnnotationHPAEnabled:     "true",
				AnnotationHPAMinReplicas: "3",
				AnnotationHPAMaxReplicas: "25",
				annotationHPACPUTargetUS: "65",
			},
			expected: &ScalingConfig{
				Mode: "hpa-controller",
				HPAConfig: &HPAConfig{
					MinReplicas:    3,
					MaxReplicas:    25,
					CPUTargetValue: 65,
				},
			},
			expectError: false,
		},
		{
			name: "HPA with min > max",
			annotations: map[string]string{
				AnnotationHPAEnabled:     "true",
				AnnotationHPAMinReplicas: "10",
				AnnotationHPAMaxReplicas: "5",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "HPA with invalid CPU target",
			annotations: map[string]string{
				AnnotationHPAEnabled:   "true",
				AnnotationHPACPUTarget: "150",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "HPA with CPU target zero",
			annotations: map[string]string{
				AnnotationHPAEnabled:   "true",
				AnnotationHPACPUTarget: "0",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "HPA takes precedence over static",
			annotations: map[string]string{
				AnnotationHPAEnabled:      "true",
				AnnotationDefaultReplicas: "10",
			},
			expected: &ScalingConfig{
				Mode: "hpa-controller",
				HPAConfig: &HPAConfig{
					MinReplicas:    defaultHPAMinReplicas,
					MaxReplicas:    defaultHPAMaxReplicas,
					CPUTargetValue: defaultCPUTarget,
				},
			},
			expectError: false,
		},
		{
			name: "HPA disabled explicitly",
			annotations: map[string]string{
				AnnotationHPAEnabled: "false",
			},
			expected:    &ScalingConfig{Mode: "none"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := gwv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-gateway",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}

			result, err := ParseScalingAnnotations(gateway, log)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expected.Mode, result.Mode)

			if tt.expected.StaticReplicas != nil {
				require.NotNil(t, result.StaticReplicas)
				require.Equal(t, *tt.expected.StaticReplicas, *result.StaticReplicas)
			}

			if tt.expected.HPAConfig != nil {
				require.NotNil(t, result.HPAConfig)
				require.Equal(t, tt.expected.HPAConfig.MinReplicas, result.HPAConfig.MinReplicas)
				require.Equal(t, tt.expected.HPAConfig.MaxReplicas, result.HPAConfig.MaxReplicas)
				require.Equal(t, tt.expected.HPAConfig.CPUTargetValue, result.HPAConfig.CPUTargetValue)
			}
		})
	}
}

func TestParseHPAAnnotations(t *testing.T) {
	log := logr.Discard()

	tests := []struct {
		name        string
		annotations map[string]string
		expected    *HPAConfig
		expectError bool
	}{
		{
			name:        "defaults when no annotations",
			annotations: map[string]string{},
			expected: &HPAConfig{
				MinReplicas:    defaultHPAMinReplicas,
				MaxReplicas:    defaultHPAMaxReplicas,
				CPUTargetValue: defaultCPUTarget,
			},
			expectError: false,
		},
		{
			name: "custom min replicas",
			annotations: map[string]string{
				AnnotationHPAMinReplicas: "5",
			},
			expected: &HPAConfig{
				MinReplicas:    5,
				MaxReplicas:    defaultHPAMaxReplicas,
				CPUTargetValue: defaultCPUTarget,
			},
			expectError: false,
		},
		{
			name: "custom max replicas",
			annotations: map[string]string{
				AnnotationHPAMaxReplicas: "100",
			},
			expected: &HPAConfig{
				MinReplicas:    defaultHPAMinReplicas,
				MaxReplicas:    100,
				CPUTargetValue: defaultCPUTarget,
			},
			expectError: false,
		},
		{
			name: "all custom values",
			annotations: map[string]string{
				AnnotationHPAMinReplicas: "3",
				AnnotationHPAMaxReplicas: "30",
				AnnotationHPACPUTarget:   "60",
			},
			expected: &HPAConfig{
				MinReplicas:    3,
				MaxReplicas:    30,
				CPUTargetValue: 60,
			},
			expectError: false,
		},
		{
			name: "legacy hyphenated CPU target spelling",
			annotations: map[string]string{
				annotationHPACPUTargetUS: "55",
			},
			expected: &HPAConfig{
				MinReplicas:    defaultHPAMinReplicas,
				MaxReplicas:    defaultHPAMaxReplicas,
				CPUTargetValue: 55,
			},
			expectError: false,
		},
		{
			name: "invalid min replicas - not a number",
			annotations: map[string]string{
				AnnotationHPAMinReplicas: "abc",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "invalid min replicas - zero",
			annotations: map[string]string{
				AnnotationHPAMinReplicas: "0",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "invalid max replicas - not a number",
			annotations: map[string]string{
				AnnotationHPAMaxReplicas: "xyz",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "min greater than max",
			annotations: map[string]string{
				AnnotationHPAMinReplicas: "20",
				AnnotationHPAMaxReplicas: "10",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "CPU target out of range - too high",
			annotations: map[string]string{
				AnnotationHPACPUTarget: "101",
			},
			expected:    nil,
			expectError: true,
		},
		{
			name: "CPU target out of range - too low",
			annotations: map[string]string{
				AnnotationHPACPUTarget: "0",
			},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseHPAAnnotations(tt.annotations, log)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expected.MinReplicas, result.MinReplicas)
			require.Equal(t, tt.expected.MaxReplicas, result.MaxReplicas)
			require.Equal(t, tt.expected.CPUTargetValue, result.CPUTargetValue)
		})
	}
}

func TestResolvedDeploymentReplicas(t *testing.T) {
	max := int32(8)
	min := int32(1)
	defaultInstances := int32(15)
	gcc := v1alpha1.GatewayClassConfig{
		Spec: v1alpha1.GatewayClassConfigSpec{
			DeploymentSpec: v1alpha1.DeploymentSpec{
				DefaultInstances: &defaultInstances,
				MaxInstances:     &max,
				MinInstances:     &min,
			},
		},
	}

	tests := []struct {
		name            string
		scalingConfig   *ScalingConfig
		currentReplicas *int32
		expected        int32
	}{
		{
			name: "unmanaged scaling preserves current replicas",
			scalingConfig: &ScalingConfig{
				Mode: "none",
			},
			currentReplicas: int32Ptr(12),
			expected:        12,
		},
		{
			name: "unmanaged scaling seeds new deployment at default replica count",
			scalingConfig: &ScalingConfig{
				Mode: "none",
			},
			expected: 1,
		},
		{
			name: "gateway class fallback seeds new deployment using deprecated bounds",
			scalingConfig: &ScalingConfig{
				Mode:                    "static",
				UseGatewayClassFallback: true,
			},
			expected: 8,
		},
		{
			name: "gateway class fallback preserves current replicas above deprecated max",
			scalingConfig: &ScalingConfig{
				Mode:                    "static",
				UseGatewayClassFallback: true,
			},
			currentReplicas: int32Ptr(15),
			expected:        15,
		},
		{
			name: "controller HPA preserves current replicas above deprecated max",
			scalingConfig: &ScalingConfig{
				Mode: "hpa-controller",
				HPAConfig: &HPAConfig{
					MinReplicas: 2,
				},
			},
			currentReplicas: int32Ptr(15),
			expected:        15,
		},
		{
			name: "controller HPA seeds new deployment from min replicas",
			scalingConfig: &ScalingConfig{
				Mode: "hpa-controller",
				HPAConfig: &HPAConfig{
					MinReplicas: 2,
				},
			},
			expected: 2,
		},
		{
			name: "gateway static annotation ignores deprecated max",
			scalingConfig: &ScalingConfig{
				Mode:           "static",
				StaticReplicas: int32Ptr(20),
			},
			expected: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicas := resolvedDeploymentReplicas(tt.scalingConfig, gcc, tt.currentReplicas)
			require.NotNil(t, replicas)
			require.Equal(t, tt.expected, *replicas)
		})
	}
}

// Helper function to create int32 pointer.
func int32Ptr(i int32) *int32 {
	return &i
}

func TestScalingAnnotationsConfigured(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "nil annotations returns false",
			annotations: nil,
			expected:    false,
		},
		{
			name:        "empty annotations returns false",
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name: "default-replicas annotation is recognized",
			annotations: map[string]string{
				AnnotationDefaultReplicas: "5",
			},
			expected: true,
		},
		{
			name: "hpa-enabled annotation is recognized",
			annotations: map[string]string{
				AnnotationHPAEnabled: "true",
			},
			expected: true,
		},
		{
			name: "hpa-minimum-replicas annotation is recognized",
			annotations: map[string]string{
				AnnotationHPAMinReplicas: "2",
			},
			expected: true,
		},
		{
			name: "hpa-maximum-replicas annotation is recognized",
			annotations: map[string]string{
				AnnotationHPAMaxReplicas: "20",
			},
			expected: true,
		},
		{
			name: "hpa-cpu-utilisation-target annotation is recognized",
			annotations: map[string]string{
				AnnotationHPACPUTarget: "80",
			},
			expected: true,
		},
		{
			name: "legacy hyphenated hpa-cpu-utilization-target annotation is recognized",
			annotations: map[string]string{
				annotationHPACPUTargetUS: "70",
			},
			expected: true,
		},
		{
			name: "unrelated annotation returns false",
			annotations: map[string]string{
				"some.other/annotation": "value",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := gwv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-gateway",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}
			require.Equal(t, tt.expected, scalingAnnotationsConfigured(gateway))
		})
	}
}

func TestResolvedDeploymentReplicas_EdgeCases(t *testing.T) {
	gcc := v1alpha1.GatewayClassConfig{
		Spec: v1alpha1.GatewayClassConfigSpec{
			DeploymentSpec: v1alpha1.DeploymentSpec{
				DefaultInstances: int32Ptr(3),
				MaxInstances:     int32Ptr(8),
				MinInstances:     int32Ptr(1),
			},
		},
	}

	tests := []struct {
		name            string
		scalingConfig   *ScalingConfig
		currentReplicas *int32
		expected        int32
	}{
		{
			name:            "nil scalingConfig falls back to deploymentReplicas with gcc default",
			scalingConfig:   nil,
			currentReplicas: nil,
			expected:        3,
		},
		{
			name:          "nil scalingConfig with current replicas above gcc max clamps to max",
			scalingConfig: nil,
			// currentReplicas is above gcc.MaxInstances=8 so deploymentReplicas clamps to 8
			currentReplicas: int32Ptr(15),
			expected:        8,
		},
		{
			name: "hpa-user mode with nil currentReplicas seeds at default 1",
			scalingConfig: &ScalingConfig{
				Mode: "hpa-user",
			},
			currentReplicas: nil,
			expected:        1,
		},
		{
			name: "hpa-user mode preserves current replicas regardless of gcc bounds",
			scalingConfig: &ScalingConfig{
				Mode: "hpa-user",
			},
			currentReplicas: int32Ptr(20),
			expected:        20,
		},
		{
			name: "static mode with nil StaticReplicas and no fallback seeds at default 1",
			scalingConfig: &ScalingConfig{
				Mode:           "static",
				StaticReplicas: nil,
			},
			currentReplicas: nil,
			expected:        1,
		},
		{
			name: "static annotation allows replicas well beyond the previous hard limit of 8",
			scalingConfig: &ScalingConfig{
				Mode:           "static",
				StaticReplicas: int32Ptr(16),
			},
			currentReplicas: nil,
			expected:        16,
		},
		{
			name: "unknown mode falls back to default 1",
			scalingConfig: &ScalingConfig{
				Mode: "unknown-mode",
			},
			currentReplicas: nil,
			expected:        1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicas := resolvedDeploymentReplicas(tt.scalingConfig, gcc, tt.currentReplicas)
			require.NotNil(t, replicas)
			require.Equal(t, tt.expected, *replicas)
		})
	}
}
