// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package aicleanup implements the "ai-cleanup" subcommand.
// It is invoked by a pre-upgrade/pre-delete Helm hook when ai.enabled is
// flipped to false. It deletes all InferenceGateway, InferenceModelConfig,
// McpServerConfig, AgentConfig, and InferencePoolConfig CRs and waits for
// each controller to finish its finalizer-based cleanup (Consul config entry
// deletion, catalog deregistration, owned-resource GC) before returning.
// Helm can then remove the CRDs cleanly. If none of the CRDs exist in the
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

	// InferenceGateways own a Deployment and Service via ownerReferences, and
	// their controller deletes the Consul config entry + catalog registrations
	// during the finalizer path. Delete them first so the controller can run
	// its full cleanup before the pool configs are removed.
	if err := c.cleanupInferenceGateways(); err != nil {
		c.UI.Error(err.Error())
		return 1
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

// cleanupInferenceGateways deletes all InferenceGateway CRs and waits for each
// one to be fully removed. The InferenceGatewayController handles its own
// finalizer: it deletes the Consul AIGateway config entry and deregisters pods
// from the catalog before removing the finalizer and allowing K8s to GC the
// owned Deployment and Service. We must not strip the finalizer manually —
// doing so would bypass that cleanup and leave stale state in Consul.
func (c *Command) cleanupInferenceGateways() error {
	list := &v1alpha1.InferenceGatewayList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("InferenceGateway CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list InferenceGateways: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d InferenceGateway(s), deleting and waiting for controller cleanup.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.deleteAndWait(obj); err != nil {
			return fmt.Errorf("cleanup InferenceGateway %q in namespace %q: %w", obj.Name, obj.Namespace, err)
		}
	}
	return nil
}

// cleanupInferenceModelConfigs deletes all InferenceModelConfig CRs and waits
// for each one to be fully removed. If the CRD is not registered the function
// returns nil immediately.
func (c *Command) cleanupInferenceModelConfigs() error {
	list := &v1alpha1.InferenceModelConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("InferenceModelConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list InferenceModelConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d InferenceModelConfig(s), deleting and waiting for controller cleanup.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.deleteAndWait(obj); err != nil {
			return fmt.Errorf("cleanup InferenceModelConfig %q: %w", obj.Name, err)
		}
	}
	return nil
}

// cleanupMcpServerConfigs deletes all McpServerConfig CRs and waits for each
// one to be fully removed. If the CRD is not registered the function returns
// nil immediately.
func (c *Command) cleanupMcpServerConfigs() error {
	list := &v1alpha1.McpServerConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("McpServerConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list McpServerConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d McpServerConfig(s), deleting and waiting for controller cleanup.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.deleteAndWait(obj); err != nil {
			return fmt.Errorf("cleanup McpServerConfig %q: %w", obj.Name, err)
		}
	}
	return nil
}

// cleanupAgentConfigs deletes all AgentConfig CRs and waits for each one to be
// fully removed. If the CRD is not registered the function returns nil
// immediately.
func (c *Command) cleanupAgentConfigs() error {
	list := &v1alpha1.AgentConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("AgentConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list AgentConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d AgentConfig(s), deleting and waiting for controller cleanup.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.deleteAndWait(obj); err != nil {
			return fmt.Errorf("cleanup AgentConfig %q: %w", obj.Name, err)
		}
	}
	return nil
}

// cleanupInferencePoolConfigs deletes all InferencePoolConfig CRs and waits
// for each one to be fully removed. If the CRD is not registered the function
// returns nil immediately.
func (c *Command) cleanupInferencePoolConfigs() error {
	list := &v1alpha1.InferencePoolConfigList{}
	if err := c.k8sClient.List(c.ctx, list); err != nil {
		if isCRDAbsent(err) {
			c.UI.Info("InferencePoolConfig CRD not found, skipping.")
			return nil
		}
		return fmt.Errorf("list InferencePoolConfigs: %w", err)
	}

	c.UI.Info(fmt.Sprintf("Found %d InferencePoolConfig(s), deleting and waiting for controller cleanup.", len(list.Items)))
	for i := range list.Items {
		obj := &list.Items[i]
		if err := c.deleteAndWait(obj); err != nil {
			return fmt.Errorf("cleanup InferencePoolConfig %q in namespace %q: %w", obj.Name, obj.Namespace, err)
		}
	}
	return nil
}

// deleteAndWait issues a Delete for obj and polls until the object is fully
// gone from the API server. It does NOT touch finalizers — the owning
// controller is responsible for removing them after completing its cleanup
// (e.g. deleting the Consul config entry). The backoff gives the controller
// enough time to finish before we give up.
func (c *Command) deleteAndWait(obj client.Object) error {
	c.UI.Info(fmt.Sprintf("  deleting %s/%s", obj.GetNamespace(), obj.GetName()))

	if err := c.k8sClient.Delete(c.ctx, obj); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete: %w", err)
	}

	// Poll until the object is fully gone (finalizer removed by the controller).
	key := client.ObjectKeyFromObject(obj)
	return backoff.Retry(func() error {
		if err := c.k8sClient.Get(c.ctx, key, obj); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return errors.New("object still exists, waiting for controller to finish cleanup")
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

const synopsis = "Delete AI CRs and wait for controller-driven cleanup prior to CRD removal."
const help = `
Usage: consul-k8s-control-plane ai-cleanup [options]

  Deletes all InferenceGateway, InferenceModelConfig, McpServerConfig,
  AgentConfig, and InferencePoolConfig custom resources and waits for each
  controller to finish its finalizer-based cleanup (Consul config entry
  deletion, catalog deregistration, owned-resource GC) before returning.
  Helm can then cleanly remove the CRDs when ai.enabled is set to false.
  If none of the CRDs exist the command exits successfully. This command is
  idempotent and safe to run multiple times.

`
