// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	gwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	AnnotationDefaultReplicas = "consul.hashicorp.com/default-replicas"
	AnnotationHPAEnabled      = "consul.hashicorp.com/hpa-enabled"
	AnnotationHPAMinReplicas  = "consul.hashicorp.com/hpa-minimum-replicas"
	AnnotationHPAMaxReplicas  = "consul.hashicorp.com/hpa-maximum-replicas"
	AnnotationHPACPUTarget    = "consul.hashicorp.com/hpa-cpu-utilisation-target"

	annotationHPACPUTargetUS = "consul.hashicorp.com/hpa-cpu-utilization-target"

	defaultHPAMinReplicas = 1
	defaultHPAMaxReplicas = 10
	defaultCPUTarget      = 80
)

type ScalingConfig struct {
	Mode string

	StaticReplicas *int32

	HPAConfig *HPAConfig

	UseGatewayClassFallback bool
}

type HPAConfig struct {
	MinReplicas    int32
	MaxReplicas    int32
	CPUTargetValue int32
}

func ParseScalingAnnotations(gateway gwv1beta1.Gateway, log logr.Logger) (*ScalingConfig, error) {
	annotations := gateway.Annotations
	if annotations == nil {
		return &ScalingConfig{Mode: "none"}, nil
	}

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

func parseHPAAnnotations(annotations map[string]string, _ logr.Logger) (*HPAConfig, error) {
	config := &HPAConfig{
		MinReplicas:    defaultHPAMinReplicas,
		MaxReplicas:    defaultHPAMaxReplicas,
		CPUTargetValue: defaultCPUTarget,
	}

	if minStr, ok := annotationValue(annotations, AnnotationHPAMinReplicas); ok {
		min, err := strconv.ParseInt(minStr, 10, 32)
		if err == nil && min >= 1 {
			config.MinReplicas = int32(min)
		} else {
			return nil, fmt.Errorf("invalid %s: expected integer value greater than or equal to 1", AnnotationHPAMinReplicas)
		}
	}

	if maxStr, ok := annotationValue(annotations, AnnotationHPAMaxReplicas); ok {
		max, err := strconv.ParseInt(maxStr, 10, 32)
		if err == nil && max >= 1 {
			config.MaxReplicas = int32(max)
		} else {
			return nil, fmt.Errorf("invalid %s: expected integer value greater than or equal to 1", AnnotationHPAMaxReplicas)
		}
	}

	if config.MinReplicas <= config.MaxReplicas {
		if cpuStr, ok := annotationValue(annotations, AnnotationHPACPUTarget, annotationHPACPUTargetUS); ok {
			cpu, err := strconv.ParseInt(cpuStr, 10, 32)
			if err == nil && cpu >= 1 && cpu <= 100 {
				config.CPUTargetValue = int32(cpu)
			} else {
				return nil, fmt.Errorf("invalid %s: expected value between 1 and 100", AnnotationHPACPUTarget)
			}
		}
		return config, nil
	}

	return nil, fmt.Errorf("invalid HPA replica range: expected %s <= %s",
		AnnotationHPAMinReplicas, AnnotationHPAMaxReplicas)
}

func annotationValue(annotations map[string]string, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := annotations[key]; ok {
			return value, true
		}
	}
	return "", false
}

func scalingAnnotationsConfigured(gateway gwv1beta1.Gateway) bool {
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

func logScalingFeatureDisabled(log logr.Logger, gateway gwv1beta1.Gateway) {
	if !scalingAnnotationsConfigured(gateway) {
		return
	}

	log.Info("Ignoring Gateway scaling annotations because Enterprise API Gateway scaling is disabled. "+
		"Enable connectInject.apiGateway.managedGatewayClass.scaling.enabled to allow annotation-driven scaling and HPA reconciliation.",
		"gateway", client.ObjectKeyFromObject(&gateway))
}

func (g *Gatekeeper) DetectUserManagedHPA(ctx context.Context, gateway gwv1beta1.Gateway) (bool, error) {
	hpaList := &autoscalingv2.HorizontalPodAutoscalerList{}
	err := g.Client.List(ctx, hpaList, client.InNamespace(gateway.Namespace))
	if err != nil {
		return false, err
	}

	for _, hpa := range hpaList.Items {
		if hpa.Spec.ScaleTargetRef.Kind == "Deployment" && hpa.Spec.ScaleTargetRef.Name == gateway.Name {
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

func (g *Gatekeeper) UpsertHPA(ctx context.Context, gateway gwv1beta1.Gateway, config *HPAConfig) error {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-hpa", gateway.Name),
			Namespace: gateway.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, g.Client, hpa, func() error {
		if err := ctrl.SetControllerReference(&gateway, hpa, g.Client.Scheme()); err != nil {
			return err
		}

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

func (g *Gatekeeper) DeleteHPA(ctx context.Context, gateway gwv1beta1.Gateway) error {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	hpaName := fmt.Sprintf("%s-hpa", gateway.Name)

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

	if !metav1.IsControlledBy(hpa, &gateway) {
		g.Log.V(1).Info("HPA exists but is not controller-managed, skipping deletion",
			"gateway", gateway.Name, "hpa", hpaName)
		return nil
	}

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

func (g *Gatekeeper) DetermineScalingMode(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig) (*ScalingConfig, error) {
	LogDeprecationWarnings(gcc, g.Log)

	hasUserHPA, err := g.DetectUserManagedHPA(ctx, gateway)
	if err != nil {
		return nil, fmt.Errorf("failed to detect user-managed HPA: %w", err)
	}

	if hasUserHPA {
		g.Log.V(1).Info("User-managed HPA detected, controller will not manage replicas", "gateway", gateway.Name)
		return &ScalingConfig{Mode: "hpa-user"}, nil
	}

	config, err := ParseScalingAnnotations(gateway, g.Log)
	if err != nil {
		return nil, err
	}

	if config.Mode != "none" {
		return config, nil
	}

	if gcc.Spec.DeploymentSpec.DefaultInstances != nil ||
		gcc.Spec.DeploymentSpec.MaxInstances != nil ||
		gcc.Spec.DeploymentSpec.MinInstances != nil {
		return &ScalingConfig{
			Mode:                    "static",
			StaticReplicas:          gcc.Spec.DeploymentSpec.DefaultInstances,
			UseGatewayClassFallback: true,
		}, nil
	}

	return &ScalingConfig{Mode: "none"}, nil
}

func (g *Gatekeeper) ReconcileScaling(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig) (*ScalingConfig, error) {
	scalingConfig, err := g.DetermineScalingMode(ctx, gateway, gcc)
	if err != nil {
		return nil, err
	}

	switch scalingConfig.Mode {
	case "hpa-user":
		if err := g.DeleteHPA(ctx, gateway); err != nil {
			g.Log.Error(err, "failed to delete controller-managed HPA")
		}
	case "hpa-controller":
		if err := g.UpsertHPA(ctx, gateway, scalingConfig.HPAConfig); err != nil {
			return nil, err
		}
	case "static", "none":
		if err := g.DeleteHPA(ctx, gateway); err != nil {
			g.Log.Error(err, "failed to delete controller-managed HPA")
		}
	}

	return scalingConfig, nil
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
