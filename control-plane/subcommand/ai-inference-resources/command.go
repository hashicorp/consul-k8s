// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package aiinferenceresources implements the "ai-inference-resources" subcommand.
// It is invoked by a post-install/post-upgrade Helm Job and is responsible for
// creating or updating the InferenceModelConfig CR from values passed as CLI
// flags (which Helm renders from values.yaml).
//
// This mirrors exactly how the "gateway-resources" subcommand bootstraps
// GatewayClassConfig: Helm values → CLI flags → Go struct → k8sClient.Create/Update.
package aiinferenceresources

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

// Command is the "ai-inference-resources" subcommand.  It runs once at
// post-install/post-upgrade and upserts the InferenceModelConfig CR from the
// Helm values baked into its CLI flags.
type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	// Helm identity labels.
	flagHeritage  string
	flagChart     string
	flagApp       string
	flagRelease   string
	flagComponent string

	// Name of the InferenceModelConfig CR to create/update.
	flagConfigName string

	// Mirrors ai.inferenceModel.enabled from values.yaml.
	flagEnabled bool

	// Mirrors ai.inferenceModel.defaults.* from values.yaml.
	flagInterceptorPort   int
	flagInferencePath     string
	flagInferenceProtocol string

	// Resource strings; passed as "<quantity>" strings, e.g. "256Mi".
	flagRequestsMemory string
	flagRequestsCPU    string
	flagLimitsMemory   string
	flagLimitsCPU      string

	k8sClient client.Client

	once sync.Once
	help string
	ctx  context.Context
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagConfigName, "config-name", "consul-ai-gateway",
		"Name of the InferenceModelConfig CR to create or update.")
	c.flags.StringVar(&c.flagHeritage, "heritage", "", "Helm chart heritage for created objects.")
	c.flags.StringVar(&c.flagChart, "chart", "", "Helm chart name for created objects.")
	c.flags.StringVar(&c.flagApp, "app", "", "Helm chart app for created objects.")
	c.flags.StringVar(&c.flagRelease, "release-name", "", "Helm release name for created objects.")
	c.flags.StringVar(&c.flagComponent, "component", "ai-inference-model", "Helm chart component for created objects.")

	c.flags.BoolVar(&c.flagEnabled, "enabled", false,
		"Whether the AI inference interceptor feature is active (mirrors ai.inferenceModel.enabled).")
	c.flags.IntVar(&c.flagInterceptorPort, "interceptor-port", 21101,
		"TCP port the interceptor container listens on inside the pod.")
	c.flags.StringVar(&c.flagInferencePath, "inference-path", "/v1",
		"Base URL path forwarded to the upstream LLM endpoint.")
	c.flags.StringVar(&c.flagInferenceProtocol, "inference-protocol", "openai",
		"Wire protocol for the upstream LLM (openai|anthropic|bedrock).")
	c.flags.StringVar(&c.flagRequestsMemory, "requests-memory", "256Mi",
		"Memory request for the interceptor init-container.")
	c.flags.StringVar(&c.flagRequestsCPU, "requests-cpu", "500m",
		"CPU request for the interceptor init-container.")
	c.flags.StringVar(&c.flagLimitsMemory, "limits-memory", "512Mi",
		"Memory limit for the interceptor init-container.")
	c.flags.StringVar(&c.flagLimitsCPU, "limits-cpu", "1000m",
		"CPU limit for the interceptor init-container.")

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	// Build the Kubernetes client if not already injected (e.g. in tests).
	if c.k8sClient == nil {
		cfg, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}
		s := runtime.NewScheme()
		if err := clientgoscheme.AddToScheme(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add client-go scheme: %s", err))
			return 1
		}
		if err := v1alpha1.AddToScheme(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add consul-k8s scheme: %s", err))
			return 1
		}
		c.k8sClient, err = client.New(cfg, client.Options{Scheme: s})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	// Parse resource quantities.
	resources, err := c.buildResources()
	if err != nil {
		c.UI.Error(fmt.Sprintf("Invalid resource quantity: %s", err))
		return 1
	}

	labels := map[string]string{
		"app":       c.flagApp,
		"chart":     c.flagChart,
		"heritage":  c.flagHeritage,
		"release":   c.flagRelease,
		"component": c.flagComponent,
	}

	// Build the desired InferenceModelConfig from the Helm-supplied flags.
	desired := &v1alpha1.InferenceModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:   c.flagConfigName,
			Labels: labels,
		},
		Spec: v1alpha1.InferenceModelConfigSpec{
			Enabled: c.flagEnabled,
			Defaults: v1alpha1.InferenceModelDefaults{
				InterceptorPort:   int32(c.flagInterceptorPort),
				InferencePath:     c.flagInferencePath,
				InferenceProtocol: c.flagInferenceProtocol,
				Resources:         resources,
			},
		},
	}

	if err := forceInferenceModelConfig(c.ctx, c.k8sClient, desired); err != nil {
		c.UI.Error(fmt.Sprintf("Error upserting InferenceModelConfig %q: %s", c.flagConfigName, err))
		return 1
	}

	c.UI.Info(fmt.Sprintf("InferenceModelConfig %q successfully created/updated", c.flagConfigName))
	return 0
}

// forceInferenceModelConfig upserts the InferenceModelConfig CR with exponential
// backoff, identical in structure to forceClassConfig in gateway-resources.
func forceInferenceModelConfig(ctx context.Context, k8sClient client.Client, desired *v1alpha1.InferenceModelConfig) error {
	return backoff.Retry(func() error {
		var existing v1alpha1.InferenceModelConfig
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		if k8serrors.IsNotFound(err) {
			return k8sClient.Create(ctx, desired)
		}
		// CR already exists — update Spec and Labels so Helm upgrades propagate.
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		return k8sClient.Update(ctx, &existing)
	}, exponentialBackoff())
}

// buildResources parses the four resource flag strings into a
// corev1.ResourceRequirements struct.
func (c *Command) buildResources() (corev1.ResourceRequirements, error) {
	reqMem, err := resource.ParseQuantity(c.flagRequestsMemory)
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("requests-memory %q: %w", c.flagRequestsMemory, err)
	}
	reqCPU, err := resource.ParseQuantity(c.flagRequestsCPU)
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("requests-cpu %q: %w", c.flagRequestsCPU, err)
	}
	limMem, err := resource.ParseQuantity(c.flagLimitsMemory)
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("limits-memory %q: %w", c.flagLimitsMemory, err)
	}
	limCPU, err := resource.ParseQuantity(c.flagLimitsCPU)
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("limits-cpu %q: %w", c.flagLimitsCPU, err)
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: reqMem,
			corev1.ResourceCPU:    reqCPU,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: limMem,
			corev1.ResourceCPU:    limCPU,
		},
	}, nil
}

func (c *Command) validateFlags() error {
	if c.flagConfigName == "" {
		return errors.New("-config-name must be set")
	}
	if c.flagHeritage == "" {
		return errors.New("-heritage must be set")
	}
	if c.flagChart == "" {
		return errors.New("-chart must be set")
	}
	if c.flagApp == "" {
		return errors.New("-app must be set")
	}
	if c.flagRelease == "" {
		return errors.New("-release-name must be set")
	}
	switch c.flagInferenceProtocol {
	case "openai", "anthropic", "bedrock":
		// valid
	default:
		return fmt.Errorf("-inference-protocol must be one of openai|anthropic|bedrock, got %q", c.flagInferenceProtocol)
	}
	if c.flagInterceptorPort < 1024 || c.flagInterceptorPort > 65535 {
		return fmt.Errorf("-interceptor-port must be between 1024 and 65535, got %d", c.flagInterceptorPort)
	}
	return nil
}

func exponentialBackoff() *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = 10 * time.Second
	b.MaxInterval = 1 * time.Second
	b.Reset()
	return b
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const (
	synopsis = "Create or update the InferenceModelConfig CR after Helm install/upgrade."
	help     = `
Usage: consul-k8s-control-plane ai-inference-resources [options]

  Upserts the InferenceModelConfig custom resource from the values supplied by
  the Helm chart. Intended to be run as a post-install/post-upgrade Helm Job so
  that the InferenceModelConfig CR is always in sync with values.yaml.

`
)
