// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinject

import (
	"context"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	authv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/auth/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/endpointsv2"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/pod"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/serviceaccount"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/namespace"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/webhook"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/webhookv2"
	resourceControllers "github.com/hashicorp/consul-k8s/control-plane/controllers/resources"
	"github.com/hashicorp/consul-k8s/control-plane/gateways"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	namespacev2 "github.com/hashicorp/consul-k8s/control-plane/tenancy/namespace"
)

func (c *Command) configureV2Controllers(ctx context.Context, mgr manager.Manager, watcher *discovery.Watcher) error {
	// Create Consul API config object.
	consulConfig := c.consul.ConsulClientConfig()

	// Convert allow/deny lists to sets.
	allowK8sNamespaces := flags.ToSet(c.flagAllowK8sNamespacesList)
	denyK8sNamespaces := flags.ToSet(c.flagDenyK8sNamespacesList)
	k8sNsConfig := common.K8sNamespaceConfig{
		AllowK8sNamespacesSet: allowK8sNamespaces,
		DenyK8sNamespacesSet:  denyK8sNamespaces,
	}
	consulTenancyConfig := common.ConsulTenancyConfig{
		EnableConsulPartitions:     c.flagEnablePartitions,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableK8SNSMirroring,
		NSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
		ConsulPartition:            c.consul.Partition,
	}

	lifecycleConfig := lifecycle.Config{
		DefaultEnableProxyLifecycle:         c.flagDefaultEnableSidecarProxyLifecycle,
		DefaultEnableShutdownDrainListeners: c.flagDefaultEnableSidecarProxyLifecycleShutdownDrainListeners,
		DefaultShutdownGracePeriodSeconds:   c.flagDefaultSidecarProxyLifecycleShutdownGracePeriodSeconds,
		DefaultStartupGracePeriodSeconds:    c.flagDefaultSidecarProxyLifecycleStartupGracePeriodSeconds,
		DefaultGracefulPort:                 c.flagDefaultSidecarProxyLifecycleGracefulPort,
		DefaultGracefulShutdownPath:         c.flagDefaultSidecarProxyLifecycleGracefulShutdownPath,
		DefaultGracefulStartupPath:          c.flagDefaultSidecarProxyLifecycleGracefulStartupPath,
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
		Client:                   mgr.GetClient(),
		ConsulClientConfig:       consulConfig,
		ConsulServerConnMgr:      watcher,
		K8sNamespaceConfig:       k8sNsConfig,
		ConsulTenancyConfig:      consulTenancyConfig,
		EnableTransparentProxy:   c.flagDefaultEnableTransparentProxy,
		TProxyOverwriteProbes:    c.flagTransparentProxyDefaultOverwriteProbes,
		AuthMethod:               c.flagACLAuthMethod,
		MetricsConfig:            metricsConfig,
		EnableTelemetryCollector: c.flagEnableTelemetryCollector,
		Log:                      ctrl.Log.WithName("controller").WithName("pod"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", pod.Controller{})
		return err
	}

	endpointsLogger := ctrl.Log.WithName("controller").WithName("endpoints")
	if err := (&endpointsv2.Controller{
		Client:              mgr.GetClient(),
		ConsulServerConnMgr: watcher,
		K8sNamespaceConfig:  k8sNsConfig,
		ConsulTenancyConfig: consulTenancyConfig,
		WriteCache:          endpointsv2.NewWriteCache(endpointsLogger),
		Log:                 endpointsLogger,
		Scheme:              mgr.GetScheme(),
		Context:             ctx,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", endpointsv2.Controller{})
		return err
	}

	if err := (&serviceaccount.Controller{
		Client:              mgr.GetClient(),
		ConsulServerConnMgr: watcher,
		K8sNamespaceConfig:  k8sNsConfig,
		ConsulTenancyConfig: consulTenancyConfig,
		Log:                 ctrl.Log.WithName("controller").WithName("serviceaccount"),
		Scheme:              mgr.GetScheme(),
		Context:             ctx,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", serviceaccount.Controller{})
		return err
	}

	if c.flagV2Tenancy {
		// V2 tenancy implies non-default namespaces in CE, so we don't observe flagEnableNamespaces
		err := (&namespacev2.Controller{
			Client:              mgr.GetClient(),
			ConsulServerConnMgr: watcher,
			K8sNamespaceConfig:  k8sNsConfig,
			ConsulTenancyConfig: consulTenancyConfig,
			Log:                 ctrl.Log.WithName("controller").WithName("namespacev2"),
		}).SetupWithManager(mgr)
		if err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "namespacev2")
			return err
		}
	} else {
		if c.flagEnableNamespaces {
			err := (&namespace.Controller{
				Client:                     mgr.GetClient(),
				ConsulClientConfig:         consulConfig,
				ConsulServerConnMgr:        watcher,
				AllowK8sNamespacesSet:      allowK8sNamespaces,
				DenyK8sNamespacesSet:       denyK8sNamespaces,
				ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
				EnableNSMirroring:          c.flagEnableK8SNSMirroring,
				NSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
				CrossNamespaceACLPolicy:    c.flagCrossNamespaceACLPolicy,
				Log:                        ctrl.Log.WithName("controller").WithName("namespace"),
			}).SetupWithManager(mgr)
			if err != nil {
				setupLog.Error(err, "unable to create controller", "controller", namespace.Controller{})
				return err
			}
		}
	}

	consulResourceController := &resourceControllers.ConsulResourceController{
		ConsulClientConfig:  consulConfig,
		ConsulServerConnMgr: watcher,
		ConsulTenancyConfig: consulTenancyConfig,
	}

	if err := (&resourceControllers.TrafficPermissionsController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.TrafficPermissions),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.TrafficPermissions)
		return err
	}

	if err := (&resourceControllers.GRPCRouteController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.GRPCRoute),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.GRPCRoute)
		return err
	}

	if err := (&resourceControllers.HTTPRouteController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.HTTPRoute),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.HTTPRoute)
		return err
	}

	if err := (&resourceControllers.TCPRouteController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.TCPRoute),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.TCPRoute)
		return err
	}

	if err := (&resourceControllers.ProxyConfigurationController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.ProxyConfiguration),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ProxyConfiguration)
		return err
	}

	if err := resourceControllers.RegisterGatewayFieldIndexes(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to register field indexes")
		return err
	}

	if err := (&resourceControllers.MeshConfigurationController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.MeshConfiguration),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.MeshConfiguration)
		return err
	}

	gatewayConfig := gateways.GatewayConfig{
		ConsulConfig: common.ConsulConfig{
			Address:    c.consul.Addresses,
			GRPCPort:   consulConfig.GRPCPort,
			HTTPPort:   consulConfig.HTTPPort,
			APITimeout: consulConfig.APITimeout,
		},
		ImageDataplane:      c.flagConsulDataplaneImage,
		ImageConsulK8S:      c.flagConsulK8sImage,
		ConsulTenancyConfig: consulTenancyConfig,
		PeeringEnabled:      c.flagEnablePeering,
		EnableOpenShift:     c.flagEnableOpenShift,
		AuthMethod:          c.consul.ConsulLogin.AuthMethod,
		LogLevel:            c.flagLogLevel,
		LogJSON:             c.flagLogJSON,
		TLSEnabled:          c.consul.UseTLS,
		ConsulTLSServerName: c.consul.TLSServerName,
		ConsulCACert:        string(c.caCertPem),
		SkipServerWatch:     c.consul.SkipServerWatch,
	}

	if err := (&resourceControllers.MeshGatewayController{
		Controller:    consulResourceController,
		Client:        mgr.GetClient(),
		Log:           ctrl.Log.WithName("controller").WithName(common.MeshGateway),
		Scheme:        mgr.GetScheme(),
		GatewayConfig: gatewayConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.MeshGateway)
		return err
	}

	if err := (&resourceControllers.APIGatewayController{
		Controller:    consulResourceController,
		Client:        mgr.GetClient(),
		Log:           ctrl.Log.WithName("controller").WithName(common.APIGateway),
		Scheme:        mgr.GetScheme(),
		GatewayConfig: gatewayConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.APIGateway)
		return err
	}

	if err := (&resourceControllers.GatewayClassConfigController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.GatewayClassConfig),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.GatewayClassConfig)
		return err
	}

	if err := (&resourceControllers.GatewayClassController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.GatewayClass),
		Scheme:     mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.GatewayClass)
		return err
	}

	if err := (&resourceControllers.ExportedServicesController{
		Controller: consulResourceController,
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controller").WithName(common.ExportedServices),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ExportedServices)
		return err
	}

	(&webhookv2.MeshWebhook{
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
		Log:                          ctrl.Log.WithName("handler").WithName("consul-mesh"),
		LogLevel:                     c.flagLogLevel,
		LogJSON:                      c.flagLogJSON,
	}).SetupWithManager(mgr)

	(&authv2beta1.TrafficPermissionsWebhook{
		Client:              mgr.GetClient(),
		Logger:              ctrl.Log.WithName("webhooks").WithName(common.TrafficPermissions),
		ConsulTenancyConfig: consulTenancyConfig,
	}).SetupWithManager(mgr)

	(&meshv2beta1.ProxyConfigurationWebhook{
		Client:              mgr.GetClient(),
		Logger:              ctrl.Log.WithName("webhooks").WithName(common.ProxyConfiguration),
		ConsulTenancyConfig: consulTenancyConfig,
	}).SetupWithManager(mgr)

	(&meshv2beta1.HTTPRouteWebhook{
		Client:              mgr.GetClient(),
		Logger:              ctrl.Log.WithName("webhooks").WithName(common.HTTPRoute),
		ConsulTenancyConfig: consulTenancyConfig,
	}).SetupWithManager(mgr)

	(&meshv2beta1.GRPCRouteWebhook{
		Client:              mgr.GetClient(),
		Logger:              ctrl.Log.WithName("webhooks").WithName(common.GRPCRoute),
		ConsulTenancyConfig: consulTenancyConfig,
	}).SetupWithManager(mgr)

	(&meshv2beta1.TCPRouteWebhook{
		Client:              mgr.GetClient(),
		Logger:              ctrl.Log.WithName("webhooks").WithName(common.TCPRoute),
		ConsulTenancyConfig: consulTenancyConfig,
	}).SetupWithManager(mgr)

	if err := mgr.AddReadyzCheck("ready", webhook.ReadinessCheck{CertDir: c.flagCertDir}.Ready); err != nil {
		setupLog.Error(err, "unable to create readiness check")
		return err
	}

	if c.flagEnableWebhookCAUpdate {
		err := c.updateWebhookCABundle(ctx)
		if err != nil {
			setupLog.Error(err, "problem getting CA Cert")
			return err
		}
	}

	return nil
}
