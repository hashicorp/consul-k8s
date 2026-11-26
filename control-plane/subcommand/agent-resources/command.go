// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package agentresources

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

// Command is the "agent-resources" subcommand.
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

	// Name of the AgentConfig CR to create/update.
	flagConfigName string

	// Mirrors ai.agent.enabled from values.yaml.
	flagEnabled bool

	// Mirrors ai.agent.defaults.* from values.yaml.
	flagInterceptorPort int
	flagMcpPort         int
	flagHITLPort        int
	flagApprovalTimeout string

	// Resource strings.
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

	c.flags.StringVar(&c.flagConfigName, "config-name", "consul-ai-agent",
		"Name of the AgentConfig CR to create or update.")
	c.flags.StringVar(&c.flagHeritage, "heritage", "", "Helm chart heritage for created objects.")
	c.flags.StringVar(&c.flagChart, "chart", "", "Helm chart name for created objects.")
	c.flags.StringVar(&c.flagApp, "app", "", "Helm chart app for created objects.")
	c.flags.StringVar(&c.flagRelease, "release-name", "", "Helm release name for created objects.")
	c.flags.StringVar(&c.flagComponent, "component", "ai-agent", "Helm chart component for created objects.")

	c.flags.BoolVar(&c.flagEnabled, "enabled", false,
		"Whether the AI agent feature is active (mirrors ai.agent.enabled).")
	c.flags.IntVar(&c.flagInterceptorPort, "interceptor-port", 21101,
		"TCP port the agent interceptor listens on inside the pod.")
	c.flags.IntVar(&c.flagMcpPort, "mcp-port", 15101,
		"TCP port used for MCP connectivity within the pod.")
	c.flags.IntVar(&c.flagHITLPort, "hitl-port", 16101,
		"TCP port the HITL approval server listens on inside the pod.")
	c.flags.StringVar(&c.flagApprovalTimeout, "approval-timeout", "60s",
		"Duration the agent waits for human approval before timing out.")
	c.flags.StringVar(&c.flagRequestsMemory, "requests-memory", "128Mi",
		"Memory request for the agent init-container.")
	c.flags.StringVar(&c.flagRequestsCPU, "requests-cpu", "250m",
		"CPU request for the agent init-container.")
	c.flags.StringVar(&c.flagLimitsMemory, "limits-memory", "256Mi",
		"Memory limit for the agent init-container.")
	c.flags.StringVar(&c.flagLimitsCPU, "limits-cpu", "500m",
		"CPU limit for the agent init-container.")

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

	desired := &v1alpha1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:   c.flagConfigName,
			Labels: labels,
		},
		Spec: v1alpha1.AgentConfigSpec{
			Enabled: c.flagEnabled,
			Defaults: v1alpha1.AgentDefaults{
				InterceptorPort: int32(c.flagInterceptorPort),
				McpPort:         int32(c.flagMcpPort),
				HITL: v1alpha1.AgentHITL{
					Port:            int32(c.flagHITLPort),
					ApprovalTimeout: c.flagApprovalTimeout,
				},
				Resources: resources,
			},
		},
	}

	if err := forceAgentConfig(c.ctx, c.k8sClient, desired); err != nil {
		c.UI.Error(fmt.Sprintf("Error upserting AgentConfig %q: %s", c.flagConfigName, err))
		return 1
	}

	c.UI.Info(fmt.Sprintf("AgentConfig %q successfully created/updated", c.flagConfigName))
	return 0
}

// forceAgentConfig upserts the AgentConfig CR with exponential backoff.
func forceAgentConfig(ctx context.Context, k8sClient client.Client, desired *v1alpha1.AgentConfig) error {
	return backoff.Retry(func() error {
		var existing v1alpha1.AgentConfig
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		if k8serrors.IsNotFound(err) {
			return k8sClient.Create(ctx, desired)
		}
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		return k8sClient.Update(ctx, &existing)
	}, exponentialBackoff())
}

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
	if c.flagInterceptorPort < 1024 || c.flagInterceptorPort > 65535 {
		return fmt.Errorf("-interceptor-port must be between 1024 and 65535, got %d", c.flagInterceptorPort)
	}
	if c.flagMcpPort < 1024 || c.flagMcpPort > 65535 {
		return fmt.Errorf("-mcp-port must be between 1024 and 65535, got %d", c.flagMcpPort)
	}
	if c.flagHITLPort < 1024 || c.flagHITLPort > 65535 {
		return fmt.Errorf("-hitl-port must be between 1024 and 65535, got %d", c.flagHITLPort)
	}
	if c.flagApprovalTimeout == "" {
		return errors.New("-approval-timeout must be set")
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
	synopsis = "Create or update the AgentConfig CR after Helm install/upgrade."
	help     = `
Usage: consul-k8s-control-plane agent-resources [options]

  Upserts the AgentConfig custom resource from the values supplied by the
  Helm chart. Intended to be run as a post-install/post-upgrade Helm Job.

`
)
