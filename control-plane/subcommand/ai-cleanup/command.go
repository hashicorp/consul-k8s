// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package aicleanup implements the "ai-cleanup" subcommand.
// It is invoked by a pre-upgrade/pre-delete Helm hook when ai.enabled is
// flipped to false. It strips finalizers from all InferenceModelConfig,
// McpServerConfig, AgentConfig, and InferencePoolConfig CRs so that Helm can
// subsequently delete the CRDs cleanly. If none of the CRDs exist in the
// cluster the command exits successfully — it is safe to run multiple times
// (idempotent).
package aicleanup

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/mitchellh/cli"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

// Command is the "ai-cleanup" subcommand.
type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	k8sClient client.Client

	once sync.Once
	help string
	ctx  context.Context
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.flags.Parse(args); err != nil {
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

	if err := c.cleanupInferenceModelConfigs(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if err := c.cleanupMcpServerConfigs(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if err := c.cleanupAgentConfigs(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if err := c.cleanupInferencePoolConfigs(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	c.UI.Info("AI CRD cleanup complete.")
	return 0
}

// cleanupInferenceModelConfigs strips finalizers from all InferenceModelConfig
// CRs and deletes them so the CRD can be removed. If the CRD is not registered
// (IsNotFound / NoKindMatchError) the function returns nil immediately.
func (c *Command) cleanupInferenceModelConfigs() error {
	list := &v1alpha1.InferenceModelConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("InferenceModelConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list InferenceModelConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d InferenceModelConfig(s), stripping finalizers and deleting.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.stripFinalizersAndDelete(obj); err != nil {
			return fmt.Errorf("cleanup InferenceModelConfig %q: %w", obj.Name, err)
		}
	}
	return nil
}

// cleanupMcpServerConfigs strips finalizers from all McpServerConfig CRs and
// deletes them so the CRD can be removed. If the CRD is not registered the
// function returns nil immediately.
func (c *Command) cleanupMcpServerConfigs() error {
	list := &v1alpha1.McpServerConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("McpServerConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list McpServerConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d McpServerConfig(s), stripping finalizers and deleting.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.stripFinalizersAndDelete(obj); err != nil {
			return fmt.Errorf("cleanup McpServerConfig %q: %w", obj.Name, err)
		}
	}
	return nil
}

func (c *Command) cleanupAgentConfigs() error {
	list := &v1alpha1.AgentConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("AgentConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list AgentConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d AgentConfig(s), stripping finalizers and deleting.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.stripFinalizersAndDelete(obj); err != nil {
			return fmt.Errorf("cleanup AgentConfig %q: %w", obj.Name, err)
		}
	}
	return nil
}

// cleanupInferencePoolConfigs strips finalizers from all InferencePoolConfig
// CRs and deletes them so the CRD can be removed. If the CRD is not registered
// the function returns nil immediately.
func (c *Command) cleanupInferencePoolConfigs() error {
	list := &v1alpha1.InferencePoolConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("InferencePoolConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list InferencePoolConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d InferencePoolConfig(s), stripping finalizers and deleting.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.stripFinalizersAndDelete(obj); err != nil {
			return fmt.Errorf("cleanup InferencePoolConfig %q in namespace %q: %w", obj.Name, obj.Namespace, err)
		}
	}
	return nil
}

// stripFinalizersAndDelete removes all finalizers from obj (so Kubernetes
// unblocks the delete) then issues a Delete. It retries with backoff until
// the object is confirmed gone.
func (c *Command) stripFinalizersAndDelete(obj client.Object) error {
	c.UI.Info(fmt.Sprintf("  stripping finalizers from %s", obj.GetName()))

	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	obj.SetFinalizers([]string{})
	if err := c.k8sClient.Patch(c.ctx, obj, patch); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("patch finalizers: %w", err)
	}

	c.UI.Info(fmt.Sprintf("  deleting %s", obj.GetName()))
	if err := c.k8sClient.Delete(c.ctx, obj); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete: %w", err)
	}

	// Wait until the object is fully gone.
	key := client.ObjectKeyFromObject(obj)
	return backoff.Retry(func() error {
		if err := c.k8sClient.Get(c.ctx, key, obj); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return errors.New("object still exists")
	}, exponentialBackoff())
}

// isCRDAbsent returns true when the error indicates the CRD is not registered
// in the cluster — either the API group is unknown or the resource kind has no
// match. This happens when the CRD was never installed or was already deleted.
func isCRDAbsent(err error) bool {
	return k8serrors.IsNotFound(err) ||
		isNoMatchError(err)
}

// isNoMatchError detects runtime.IsNotRegisteredError and
// meta.IsNoMatchError by checking the error message, since both are
// non-exported types in controller-runtime's dependency tree.
func isNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no matches for kind") ||
		strings.Contains(msg, "no kind is registered") ||
		strings.Contains(msg, "is not registered")
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

const synopsis = "Strip finalizers and delete AI CRs prior to CRD removal."
const help = `
Usage: consul-k8s-control-plane ai-cleanup [options]

  Strips finalizers from all InferenceModelConfig, McpServerConfig, AgentConfig,
  and InferencePoolConfig custom resources then deletes them, allowing Helm to
  cleanly remove the CRDs when ai.enabled is set to false. If none of the CRDs
  exist the command exits successfully. This command is idempotent and safe to
  run multiple times.

`
