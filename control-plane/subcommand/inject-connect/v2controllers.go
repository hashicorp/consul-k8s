// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinject

import (
	"context"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlRuntimeWebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/endpointsv2"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/pod"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	webhookV2 "github.com/hashicorp/consul-k8s/control-plane/connect-inject/webhook_v2"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

func (c *Command) configureV2Controllers(ctx context.Context, mgr manager.Manager, watcher *discovery.Watcher) error {

	// Create Consul API config object.
	consulConfig := c.consul.ConsulClientConfig()

	//Convert allow/deny lists to sets.
	allowK8sNamespaces := flags.ToSet(c.flagAllowK8sNamespacesList)
	denyK8sNamespaces := flags.ToSet(c.flagDenyK8sNamespacesList)

	lifecycleConfig := lifecycle.Config{
		DefaultEnableProxyLifecycle:         c.flagDefaultEnableSidecarProxyLifecycle,
		DefaultEnableShutdownDrainListeners: c.flagDefaultEnableSidecarProxyLifecycleShutdownDrainListeners,
		DefaultShutdownGracePeriodSeconds:   c.flagDefaultSidecarProxyLifecycleShutdownGracePeriodSeconds,
		DefaultGracefulPort:                 c.flagDefaultSidecarProxyLifecycleGracefulPort,
		DefaultGracefulShutdownPath:         c.flagDefaultSidecarProxyLifecycleGracefulShutdownPath,
	}

	metricsConfig := metrics.Config{
		DefaultEnableMetrics:        c.flagDefaultEnableMetrics,
		EnableGatewayMetrics:        c.flagEnableGatewayMetrics,
		DefaultEnableMetricsMerging: c.flagDefaultEnableMetricsMerging,
		DefaultMergedMetricsPort:    c.flagDefaultMergedMetricsPort,
		DefaultPrometheusScrapePort: c.flagDefaultPrometheusScrapePort,
		DefaultPrometheusScrapePath: c.flagDefaultPrometheusScrapePath,
	}

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
		EnableTransparentProxy:     c.flagDefaultEnableTransparentProxy,
		TProxyOverwriteProbes:      c.flagTransparentProxyDefaultOverwriteProbes,
		AuthMethod:                 c.flagACLAuthMethod,
		MetricsConfig:              metricsConfig,
		EnableTelemetryCollector:   c.flagEnableTelemetryCollector,
		Log:                        ctrl.Log.WithName("controller").WithName("pods"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", pod.Controller{})
		return err
	}

	if err := (&endpointsv2.Controller{
		Client:                     mgr.GetClient(),
		ConsulServerConnMgr:        watcher,
		AllowK8sNamespacesSet:      allowK8sNamespaces,
		DenyK8sNamespacesSet:       denyK8sNamespaces,
		EnableConsulPartitions:     c.flagEnablePartitions,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableK8SNSMirroring,
		NSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
		Log:                        ctrl.Log.WithName("controller").WithName("endpoints"),
		Scheme:                     mgr.GetScheme(),
		Context:                    ctx,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", endpointsv2.Controller{})
		return err
	}

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
	mgr.GetWebhookServer().CertDir = c.flagCertDir

	mgr.GetWebhookServer().Register("/mutate",
		&ctrlRuntimeWebhook.Admission{Handler: &webhookV2.MeshWebhook{
			Clientset:                    c.clientset,
			ReleaseNamespace:             c.flagReleaseNamespace,
			ConsulConfig:                 consulConfig,
			ConsulServerConnMgr:          watcher,
			ImageConsul:                  c.flagConsulImage,
			ImageConsulDataplane:         c.flagConsulDataplaneImage,
			EnvoyExtraArgs:               c.flagEnvoyExtraArgs,
			ImageConsulK8S:               c.flagConsulK8sImage,
			RequireAnnotation:            !c.flagDefaultInject,
			AuthMethod:                   c.flagACLAuthMethod,
			ConsulCACert:                 string(c.caCertPem),
			TLSEnabled:                   c.consul.UseTLS,
			ConsulAddress:                c.consul.Addresses,
			SkipServerWatch:              c.consul.SkipServerWatch,
			ConsulTLSServerName:          c.consul.TLSServerName,
			DefaultProxyCPURequest:       c.sidecarProxyCPURequest,
			DefaultProxyCPULimit:         c.sidecarProxyCPULimit,
			DefaultProxyMemoryRequest:    c.sidecarProxyMemoryRequest,
			DefaultProxyMemoryLimit:      c.sidecarProxyMemoryLimit,
			DefaultEnvoyProxyConcurrency: c.flagDefaultEnvoyProxyConcurrency,
			LifecycleConfig:              lifecycleConfig,
			MetricsConfig:                metricsConfig,
			InitContainerResources:       c.initContainerResources,
			ConsulPartition:              c.consul.Partition,
			AllowK8sNamespacesSet:        allowK8sNamespaces,
			DenyK8sNamespacesSet:         denyK8sNamespaces,
			EnableNamespaces:             c.flagEnableNamespaces,
			ConsulDestinationNamespace:   c.flagConsulDestinationNamespace,
			EnableK8SNSMirroring:         c.flagEnableK8SNSMirroring,
			K8SNSMirroringPrefix:         c.flagK8SNSMirroringPrefix,
			CrossNamespaceACLPolicy:      c.flagCrossNamespaceACLPolicy,
			EnableTransparentProxy:       c.flagDefaultEnableTransparentProxy,
			EnableCNI:                    c.flagEnableCNI,
			TProxyOverwriteProbes:        c.flagTransparentProxyDefaultOverwriteProbes,
			EnableConsulDNS:              c.flagEnableConsulDNS,
			EnableOpenShift:              c.flagEnableOpenShift,
			Log:                          ctrl.Log.WithName("handler").WithName("connect"),
			LogLevel:                     c.flagLogLevel,
			LogJSON:                      c.flagLogJSON,
		}})

	// TODO: Update Webhook CA Bundle

	return nil
}
