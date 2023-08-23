// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinject

import (
	"context"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/pod"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

func (c *Command) configureV2Controllers(ctx context.Context, mgr manager.Manager, watcher *discovery.Watcher) error {

	// Create Consul API config object.
	consulConfig := c.consul.ConsulClientConfig()

	//Convert allow/deny lists to sets.
	allowK8sNamespaces := flags.ToSet(c.flagAllowK8sNamespacesList)
	denyK8sNamespaces := flags.ToSet(c.flagDenyK8sNamespacesList)

	//lifecycleConfig := lifecycle.Config{
	//	DefaultEnableProxyLifecycle:         c.flagDefaultEnableSidecarProxyLifecycle,
	//	DefaultEnableShutdownDrainListeners: c.flagDefaultEnableSidecarProxyLifecycleShutdownDrainListeners,
	//	DefaultShutdownGracePeriodSeconds:   c.flagDefaultSidecarProxyLifecycleShutdownGracePeriodSeconds,
	//	DefaultGracefulPort:                 c.flagDefaultSidecarProxyLifecycleGracefulPort,
	//	DefaultGracefulShutdownPath:         c.flagDefaultSidecarProxyLifecycleGracefulShutdownPath,
	//}

	//metricsConfig := metrics.Config{
	//	DefaultEnableMetrics:        c.flagDefaultEnableMetrics,
	//	EnableGatewayMetrics:        c.flagEnableGatewayMetrics,
	//	DefaultEnableMetricsMerging: c.flagDefaultEnableMetricsMerging,
	//	DefaultMergedMetricsPort:    c.flagDefaultMergedMetricsPort,
	//	DefaultPrometheusScrapePort: c.flagDefaultPrometheusScrapePort,
	//	DefaultPrometheusScrapePath: c.flagDefaultPrometheusScrapePath,
	//}

	if err := (&pod.Controller{
		Client:                     mgr.GetClient(),
		ConsulClientConfig:         consulConfig,
		ConsulServerConnMgr:        watcher,
		AllowK8sNamespacesSet:      allowK8sNamespaces,
		DenyK8sNamespacesSet:       denyK8sNamespaces,
		EnableConsulPartitions:     c.flagEnablePartitions,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableK8SNSMirroring,
		NSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
		ConsulPartition:            c.consul.Partition,
		AuthMethod:                 c.flagACLAuthMethod,
		Log:                        ctrl.Log.WithName("controller").WithName("pods"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", pod.Controller{})
		return err
	}

	// TODO: V2 Endpoints Controller

	// TODO: Nodes Controller

	// TODO: Serviceaccounts Controller

	// TODO: V2 Config Controller(s)

	// // Metadata for webhooks
	//consulMeta := apicommon.ConsulMeta{
	//	PartitionsEnabled:    c.flagEnablePartitions,
	//	Partition:            c.consul.Partition,
	//	NamespacesEnabled:    c.flagEnableNamespaces,
	//	DestinationNamespace: c.flagConsulDestinationNamespace,
	//	Mirroring:            c.flagEnableK8SNSMirroring,
	//	Prefix:               c.flagK8SNSMirroringPrefix,
	//}

	// TODO: register webhooks

	// TODO: Update Webhook CA Bundle

	return nil
}
