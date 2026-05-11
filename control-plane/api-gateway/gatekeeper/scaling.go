// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	// Annotation keys for scaling configuration.
	AnnotationDefaultReplicas = "consul.hashicorp.com/default-replicas"
	AnnotationHPAEnabled      = "consul.hashicorp.com/hpa-enabled"
	AnnotationHPAMinReplicas  = "consul.hashicorp.com/hpa-minimum-replicas"
	AnnotationHPAMaxReplicas  = "consul.hashicorp.com/hpa-maximum-replicas"
	AnnotationHPACPUTarget    = "consul.hashicorp.com/hpa-cpu-utilisation-target"

	// Backward-compatible alias for the earlier hyphenated US spelling.
	annotationHPACPUTargetUS = "consul.hashicorp.com/hpa-cpu-utilization-target"

	// Default values.
	defaultHPAMinReplicas = 1
	defaultHPAMaxReplicas = 10
	defaultCPUTarget      = 80
)

// ScalingConfig holds the parsed scaling configuration from Gateway annotations.
type ScalingConfig struct {
	// Mode indicates the scaling mode: "static", "hpa-controller", "hpa-user", or "none"
	Mode string

	// StaticReplicas is the fixed number of replicas (for static mode)
	StaticReplicas *int32

	// HPAConfig holds HPA configuration (for hpa-controller mode)
	HPAConfig *HPAConfig

	// UseGatewayClassFallback indicates the deprecated GatewayClassConfig
	// deployment fields are still the source of truth for replicas.
	UseGatewayClassFallback bool
}

// HPAConfig holds HPA-specific configuration.
type HPAConfig struct {
	MinReplicas    int32
	MaxReplicas    int32
	CPUTargetValue int32
}

// ParseScalingAnnotations extracts and validates scaling configuration from Gateway annotations.
func ParseScalingAnnotations(gateway gwv1.Gateway, log logr.Logger) (*ScalingConfig, error) {
	annotations := gateway.Annotations
	if annotations == nil {
		return &ScalingConfig{Mode: "none"}, nil
	}

	// Check for HPA enabled annotation
	if hpaEnabled, ok := annotationValue(annotations, AnnotationHPAEnabled); ok && hpaEnabled == "true" {
		hpaConfig, err := parseHPAAnnotations(annotations, log)
		if err != nil {
			return nil, fmt.Errorf("failed to parse HPA annotations: %w", err)
		}

		return &ScalingConfig{
			Mode:      "hpa-controller",
			HPAConfig: hpaConfig,
		}, nil
	}

	// Check for static replica annotation
	if replicasStr, ok := annotations[AnnotationDefaultReplicas]; ok {
		replicas, err := strconv.ParseInt(replicasStr, 10, 32)
		if err != nil {
			log.Error(err, "invalid value for default-replicas annotation, must be a positive integer",
				"annotation", AnnotationDefaultReplicas, "value", replicasStr)
			return nil, fmt.Errorf("invalid default-replicas annotation: %w", err)
		}

		if replicas < 1 {
			return nil, fmt.Errorf("default-replicas must be at least 1, got %d", replicas)
		}

		replicas32 := int32(replicas)
		return &ScalingConfig{
			Mode:           "static",
			StaticReplicas: &replicas32,
		}, nil
	}

	return &ScalingConfig{Mode: "none"}, nil
}

// parseHPAAnnotations extracts HPA configuration from annotations.
func parseHPAAnnotations(annotations map[string]string, log logr.Logger) (*HPAConfig, error) {
	config := &HPAConfig{
		MinReplicas:    defaultHPAMinReplicas,
		MaxReplicas:    defaultHPAMaxReplicas,
		CPUTargetValue: defaultCPUTarget,
	}

	// Parse min replicas
	if minStr, ok := annotationValue(annotations, AnnotationHPAMinReplicas); ok {
		min, err := strconv.ParseInt(minStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", AnnotationHPAMinReplicas, err)
		}
		if min < 1 {
			return nil, fmt.Errorf("%s must be at least 1, got %d", AnnotationHPAMinReplicas, min)
		}
		config.MinReplicas = int32(min)
	}

	// Parse max replicas
	if maxStr, ok := annotationValue(annotations, AnnotationHPAMaxReplicas); ok {
		max, err := strconv.ParseInt(maxStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", AnnotationHPAMaxReplicas, err)
		}
		if max < 1 {
			return nil, fmt.Errorf("%s must be at least 1, got %d", AnnotationHPAMaxReplicas, max)
		}
		config.MaxReplicas = int32(max)
	}

	// Validate min <= max
	if config.MinReplicas > config.MaxReplicas {
		return nil, fmt.Errorf("%s (%d) cannot be greater than %s (%d)",
			AnnotationHPAMinReplicas, config.MinReplicas, AnnotationHPAMaxReplicas, config.MaxReplicas)
	}

	// Parse CPU target
	if cpuStr, ok := annotationValue(annotations, AnnotationHPACPUTarget, annotationHPACPUTargetUS); ok {
		cpu, err := strconv.ParseInt(cpuStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", AnnotationHPACPUTarget, err)
		}
		if cpu < 1 || cpu > 100 {
			return nil, fmt.Errorf("%s must be between 1 and 100, got %d", AnnotationHPACPUTarget, cpu)
		}
		config.CPUTargetValue = int32(cpu)
	}

	return config, nil
}

func annotationValue(annotations map[string]string, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := annotations[key]; ok {
			return value, true
		}
	}
	return "", false
}

func scalingAnnotationsConfigured(gateway gwv1.Gateway) bool {
	annotations := gateway.Annotations
	if annotations == nil {
		return false
	}

	if _, ok := annotations[AnnotationDefaultReplicas]; ok {
		return true
	}
	if _, ok := annotationValue(annotations, AnnotationHPAEnabled, AnnotationHPAMinReplicas, AnnotationHPAMaxReplicas, AnnotationHPACPUTarget, annotationHPACPUTargetUS); ok {
		return true
	}

	return false
}

func logScalingFeatureDisabled(log logr.Logger, gateway gwv1.Gateway) {
	if !scalingAnnotationsConfigured(gateway) {
		return
	}

	log.Info("Ignoring Gateway scaling annotations because Enterprise API Gateway scaling is disabled. "+
		"Enable connectInject.apiGateway.managedGatewayClass.scaling.enabled to allow annotation-driven scaling and HPA reconciliation.",
		"gateway", client.ObjectKeyFromObject(&gateway))
}

// DetectUserManagedHPA checks if a user has created their own HPA for the gateway.
func (g *Gatekeeper) DetectUserManagedHPA(ctx context.Context, gateway gwv1.Gateway) (bool, error) {
	hpaList := &autoscalingv2.HorizontalPodAutoscalerList{}
	err := g.Client.List(ctx, hpaList, client.InNamespace(gateway.Namespace))
	if err != nil {
		return false, err
	}

	deploymentName := gateway.Name
	for _, hpa := range hpaList.Items {
		// Check if this HPA targets our deployment
		if hpa.Spec.ScaleTargetRef.Kind == "Deployment" &&
			hpa.Spec.ScaleTargetRef.Name == deploymentName {
			// Check if it's NOT controller-managed (no owner reference to gateway)
			isControllerManaged := false
			for _, owner := range hpa.OwnerReferences {
				if owner.Kind == "Gateway" && owner.Name == gateway.Name {
					isControllerManaged = true
					break
				}
			}

			if !isControllerManaged {
				return true, nil
			}
		}
	}

	return false, nil
}

// UpsertHPA creates or updates an HPA resource for the gateway.
func (g *Gatekeeper) UpsertHPA(ctx context.Context, gateway gwv1.Gateway, config *HPAConfig) error {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-hpa", gateway.Name),
			Namespace: gateway.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, g.Client, hpa, func() error {
		// Set owner reference
		if err := ctrl.SetControllerReference(&gateway, hpa, g.Client.Scheme()); err != nil {
			return err
		}

		// Configure HPA spec
		hpa.Spec = autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       gateway.Name,
			},
			MinReplicas: &config.MinReplicas,
			MaxReplicas: config.MaxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &config.CPUTargetValue,
						},
					},
				},
			},
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create/update HPA: %w", err)
	}

	g.Log.V(1).Info("HPA created/updated", "gateway", gateway.Name, "minReplicas", config.MinReplicas, "maxReplicas", config.MaxReplicas)
	return nil
}

// DeleteHPA removes the controller-managed HPA for a gateway.
func (g *Gatekeeper) DeleteHPA(ctx context.Context, gateway gwv1.Gateway) error {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	hpaName := fmt.Sprintf("%s-hpa", gateway.Name)

	// First, fetch the HPA to check if it exists and if we own it
	err := g.Client.Get(ctx, client.ObjectKey{
		Name:      hpaName,
		Namespace: gateway.Namespace,
	}, hpa)

	if k8serrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to get HPA: %w", err)
	}

	// Verify that this controller owns the HPA before deleting
	if !metav1.IsControlledBy(hpa, &gateway) {
		g.Log.V(1).Info("HPA exists but is not controller-managed, skipping deletion",
			"gateway", gateway.Name, "hpa", hpaName)
		return nil
	}

	// Delete the controller-managed HPA
	err = g.Client.Delete(ctx, hpa)
	if k8serrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to delete HPA: %w", err)
	}

	g.Log.V(1).Info("HPA deleted", "gateway", gateway.Name)
	return nil
}

// LogDeprecationWarnings logs warnings for deprecated GatewayClassConfig fields.
func LogDeprecationWarnings(gcc v1alpha1.GatewayClassConfig, log logr.Logger) {
	if gcc.Spec.DeploymentSpec.DefaultInstances != nil ||
		gcc.Spec.DeploymentSpec.MaxInstances != nil ||
		gcc.Spec.DeploymentSpec.MinInstances != nil {
		log.Info("DEPRECATED: GatewayClassConfig deployment fields (defaultInstances, maxInstances, minInstances) are deprecated. "+
			"Use Gateway annotations instead: consul.hashicorp.com/default-replicas for static replicas, "+
			"or consul.hashicorp.com/hpa-enabled, consul.hashicorp.com/hpa-minimum-replicas, "+
			"consul.hashicorp.com/hpa-maximum-replicas, and consul.hashicorp.com/hpa-cpu-utilisation-target for HPA configuration. "+
			"See: https://developer.hashicorp.com/consul/docs/k8s/api-gateway/scaling",
			"gatewayClassConfig", gcc.Name)
	}
}

// DetermineScalingMode determines the final scaling mode considering all sources.
func (g *Gatekeeper) DetermineScalingMode(ctx context.Context, gateway gwv1.Gateway, gcc v1alpha1.GatewayClassConfig) (*ScalingConfig, error) {
	// Log deprecation warnings for GCC fields
	LogDeprecationWarnings(gcc, g.Log)

	// Priority 1: Check for user-managed HPA
	hasUserHPA, err := g.DetectUserManagedHPA(ctx, gateway)
	if err != nil {
		return nil, fmt.Errorf("failed to detect user-managed HPA: %w", err)
	}

	if hasUserHPA {
		g.Log.V(1).Info("User-managed HPA detected, controller will not manage replicas", "gateway", gateway.Name)
		return &ScalingConfig{Mode: "hpa-user"}, nil
	}

	// Priority 2: Check Gateway annotations
	config, err := ParseScalingAnnotations(gateway, g.Log)
	if err != nil {
		return nil, err
	}

	if config.Mode != "none" {
		return config, nil
	}

	// Priority 3: Fall back to GCC (deprecated)
	if gcc.Spec.DeploymentSpec.DefaultInstances != nil ||
		gcc.Spec.DeploymentSpec.MaxInstances != nil ||
		gcc.Spec.DeploymentSpec.MinInstances != nil {
		return &ScalingConfig{
			Mode:                    "static",
			StaticReplicas:          gcc.Spec.DeploymentSpec.DefaultInstances,
			UseGatewayClassFallback: true,
		}, nil
	}

	// Default: no controller-owned scaling. New deployments seed at 1 replica,
	// and existing deployments keep their current scale to allow manual scaling.
	return &ScalingConfig{Mode: "none"}, nil
}

// ReconcileScaling handles the complete scaling reconciliation for a gateway
// and returns the resolved scaling mode after HPA side effects are applied.
func (g *Gatekeeper) ReconcileScaling(ctx context.Context, gateway gwv1.Gateway, gcc v1alpha1.GatewayClassConfig) (*ScalingConfig, error) {
	scalingConfig, err := g.DetermineScalingMode(ctx, gateway, gcc)
	if err != nil {
		return nil, err
	}

	switch scalingConfig.Mode {
	case "hpa-user":
		// User manages HPA, ensure we don't have a controller-managed HPA
		if err := g.DeleteHPA(ctx, gateway); err != nil {
			g.Log.Error(err, "failed to delete controller-managed HPA")
		}
		return scalingConfig, nil

	case "hpa-controller":
		// Create/update controller-managed HPA
		if err := g.UpsertHPA(ctx, gateway, scalingConfig.HPAConfig); err != nil {
			return nil, err
		}
		return scalingConfig, nil

	case "static":
		// Ensure no controller-managed HPA exists
		if err := g.DeleteHPA(ctx, gateway); err != nil {
			g.Log.Error(err, "failed to delete controller-managed HPA")
		}
		return scalingConfig, nil

	case "none":
		// Scaling is unmanaged by the controller. Ensure any controller-managed
		// HPA is removed and preserve the current Deployment scale.
		if err := g.DeleteHPA(ctx, gateway); err != nil {
			g.Log.Error(err, "failed to delete controller-managed HPA")
		}
		return scalingConfig, nil

	default:
		return scalingConfig, nil
	}
}

func resolvedDeploymentReplicas(scalingConfig *ScalingConfig, gcc v1alpha1.GatewayClassConfig, currentReplicas *int32) *int32 {
	if scalingConfig == nil {
		return deploymentReplicas(gcc, currentReplicas)
	}

	switch scalingConfig.Mode {
	case "none":
		if currentReplicas != nil {
			replicas := *currentReplicas
			return &replicas
		}
	case "hpa-controller":
		if currentReplicas != nil {
			replicas := *currentReplicas
			return &replicas
		}
		if scalingConfig.HPAConfig != nil {
			replicas := scalingConfig.HPAConfig.MinReplicas
			return &replicas
		}
	case "hpa-user":
		if currentReplicas != nil {
			replicas := *currentReplicas
			return &replicas
		}
	case "static":
		if scalingConfig.UseGatewayClassFallback {
			if currentReplicas != nil {
				replicas := *currentReplicas
				return &replicas
			}
			return deploymentReplicas(gcc, nil)
		}
		if scalingConfig.StaticReplicas != nil {
			replicas := *scalingConfig.StaticReplicas
			return &replicas
		}
	}

	defaultReplicas := defaultInstances
	return &defaultReplicas
}
