// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinject

import (
	"context"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func (c *Command) configureV2Controllers(ctx context.Context, mgr manager.Manager, watcher *discovery.Watcher) error {

	//resourceClient, err := consul.NewResourceServiceClient(watcher)
	//if err != nil {
	//	return fmt.Errorf("unable to create Consul resource service client: %w", err)
	//}

	//// Create Consul API config object.
	//consulConfig := c.consul.ConsulClientConfig()
	//
	////Convert allow/deny lists to sets.
	//allowK8sNamespaces := flags.ToSet(c.flagAllowK8sNamespacesList)
	//denyK8sNamespaces := flags.ToSet(c.flagDenyK8sNamespacesList)

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

	// TODO(dans): Pods Controller
	//if err := (&pod.Controller{
	//	Client:                      mgr.GetClient(),
	//	ConsulClientConfig:          consulConfig,
	//	ConsulServerConnMgr:         watcher,
	//	ConsulResourceServiceClient: client,
	//	AllowK8sNamespacesSet:       allowK8sNamespaces,
	//	DenyK8sNamespacesSet:        denyK8sNamespaces,
	//	MetricsConfig:               metricsConfig,
	//	EnableConsulPartitions:      c.flagEnablePartitions,
	//	EnableConsulNamespaces:      c.flagEnableNamespaces,
	//	ConsulDestinationNamespace:  c.flagConsulDestinationNamespace,
	//	EnableNSMirroring:           c.flagEnableK8SNSMirroring,
	//	NSMirroringPrefix:           c.flagK8SNSMirroringPrefix,
	//	EnableTransparentProxy:      c.flagDefaultEnableTransparentProxy,
	//	TProxyOverwriteProbes:       c.flagTransparentProxyDefaultOverwriteProbes,
	//	AuthMethod:                  c.flagACLAuthMethod,
	//	NodeMeta:                    c.flagNodeMeta,
	//	Log:                         ctrl.Log.WithName("controller").WithName("pods"),
	//	Scheme:                      mgr.GetScheme(),
	//	EnableTelemetryCollector:    c.flagEnableTelemetryCollector,
	//	Context:                     ctx,
	//}).SetupWithManager(mgr); err != nil {
	//	setupLog.Error(err, "unable to create controller", "controller", pod.Controller{})
	//	return err
	//}

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
