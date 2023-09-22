// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinject

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlRuntimeWebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	gatewaycommon "github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	gatewaycontrollers "github.com/hashicorp/consul-k8s/control-plane/api-gateway/controllers"
	apicommon "github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/config-entries/controllers"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/endpoints"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/peering"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/webhook"
	webhookconfiguration "github.com/hashicorp/consul-k8s/control-plane/helper/webhook-configuration"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

func (c *Command) configureV1Controllers(ctx context.Context, mgr manager.Manager, watcher *discovery.Watcher) error {
	// Create Consul API config object.
	consulConfig := c.consul.ConsulClientConfig()

	// Convert allow/deny lists to sets.
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

	if err := (&endpoints.Controller{
		Client:                     mgr.GetClient(),
		ConsulClientConfig:         consulConfig,
		ConsulServerConnMgr:        watcher,
		AllowK8sNamespacesSet:      allowK8sNamespaces,
		DenyK8sNamespacesSet:       denyK8sNamespaces,
		MetricsConfig:              metricsConfig,
		EnableConsulPartitions:     c.flagEnablePartitions,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableK8SNSMirroring,
		NSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
		CrossNSACLPolicy:           c.flagCrossNamespaceACLPolicy,
		EnableTransparentProxy:     c.flagDefaultEnableTransparentProxy,
		EnableWANFederation:        c.flagEnableFederation,
		TProxyOverwriteProbes:      c.flagTransparentProxyDefaultOverwriteProbes,
		AuthMethod:                 c.flagACLAuthMethod,
		NodeMeta:                   c.flagNodeMeta,
		Log:                        ctrl.Log.WithName("controller").WithName("endpoints"),
		Scheme:                     mgr.GetScheme(),
		ReleaseName:                c.flagReleaseName,
		ReleaseNamespace:           c.flagReleaseNamespace,
		EnableAutoEncrypt:          c.flagEnableAutoEncrypt,
		EnableTelemetryCollector:   c.flagEnableTelemetryCollector,
		Context:                    ctx,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", endpoints.Controller{})
		return err
	}

	// API Gateway Controllers
	if err := gatewaycontrollers.RegisterFieldIndexes(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to register field indexes")
		return err
	}

	if err := (&gatewaycontrollers.GatewayClassConfigController{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controller").WithName("gateways"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", gatewaycontrollers.GatewayClassConfigController{})
		return err
	}

	if err := (&gatewaycontrollers.GatewayClassController{
		ControllerName: gatewaycommon.GatewayClassControllerName,
		Client:         mgr.GetClient(),
		Log:            ctrl.Log.WithName("controllers").WithName("GatewayClass"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatewayClass")
		return err
	}

	cache, err := gatewaycontrollers.SetupGatewayControllerWithManager(ctx, mgr, gatewaycontrollers.GatewayControllerConfig{
		HelmConfig: gatewaycommon.HelmConfig{
			ConsulConfig: gatewaycommon.ConsulConfig{
				Address:    c.consul.Addresses,
				GRPCPort:   consulConfig.GRPCPort,
				HTTPPort:   consulConfig.HTTPPort,
				APITimeout: consulConfig.APITimeout,
			},
			ImageDataplane:             c.flagConsulDataplaneImage,
			ImageConsulK8S:             c.flagConsulK8sImage,
			ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
			NamespaceMirroringPrefix:   c.flagK8SNSMirroringPrefix,
			EnableNamespaces:           c.flagEnableNamespaces,
			PeeringEnabled:             c.flagEnablePeering,
			EnableOpenShift:            c.flagEnableOpenShift,
			EnableNamespaceMirroring:   c.flagEnableK8SNSMirroring,
			AuthMethod:                 c.consul.ConsulLogin.AuthMethod,
			LogLevel:                   c.flagLogLevel,
			LogJSON:                    c.flagLogJSON,
			TLSEnabled:                 c.consul.UseTLS,
			ConsulTLSServerName:        c.consul.TLSServerName,
			ConsulPartition:            c.consul.Partition,
			ConsulCACert:               string(c.caCertPem),
		},
		AllowK8sNamespacesSet:   allowK8sNamespaces,
		DenyK8sNamespacesSet:    denyK8sNamespaces,
		ConsulClientConfig:      consulConfig,
		ConsulServerConnMgr:     watcher,
		NamespacesEnabled:       c.flagEnableNamespaces,
		CrossNamespaceACLPolicy: c.flagCrossNamespaceACLPolicy,
		Partition:               c.consul.Partition,
		Datacenter:              c.consul.Datacenter,
	})
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Gateway")
		return err
	}

	go cache.Run(ctx)

	// wait for the cache to fill
	setupLog.Info("waiting for Consul cache sync")
	cache.WaitSynced(ctx)
	setupLog.Info("Consul cache synced")

	configEntryReconciler := &controllers.ConfigEntryController{
		ConsulClientConfig:         consulConfig,
		ConsulServerConnMgr:        watcher,
		DatacenterName:             c.consul.Datacenter,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableK8SNSMirroring,
		NSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
		CrossNSACLPolicy:           c.flagCrossNamespaceACLPolicy,
	}
	if err := (&controllers.ServiceDefaultsController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ServiceDefaults),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ServiceDefaults)
		return err
	}
	if err := (&controllers.ServiceResolverController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ServiceResolver),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ServiceResolver)
		return err
	}
	if err := (&controllers.ProxyDefaultsController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ProxyDefaults),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ProxyDefaults)
		return err
	}
	if err := (&controllers.MeshController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.Mesh),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.Mesh)
		return err
	}
	if err := (&controllers.ExportedServicesController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ExportedServices),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ExportedServices)
		return err
	}
	if err := (&controllers.ServiceRouterController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ServiceRouter),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ServiceRouter)
		return err
	}
	if err := (&controllers.ServiceSplitterController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ServiceSplitter),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ServiceSplitter)
		return err
	}
	if err := (&controllers.ServiceIntentionsController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ServiceIntentions),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ServiceIntentions)
		return err
	}
	if err := (&controllers.IngressGatewayController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.IngressGateway),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.IngressGateway)
		return err
	}
	if err := (&controllers.TerminatingGatewayController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.TerminatingGateway),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.TerminatingGateway)
		return err
	}
	if err := (&controllers.SamenessGroupController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.SamenessGroup),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.SamenessGroup)
		return err
	}
	if err := (&controllers.JWTProviderController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.JWTProvider),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.JWTProvider)
		return err
	}
	if err := (&controllers.ControlPlaneRequestLimitController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(apicommon.ControlPlaneRequestLimit),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", apicommon.ControlPlaneRequestLimit)
		return err
	}

	if err := mgr.AddReadyzCheck("ready", webhook.ReadinessCheck{CertDir: c.flagCertDir}.Ready); err != nil {
		setupLog.Error(err, "unable to create readiness check", "controller", endpoints.Controller{})
		return err
	}

	if c.flagEnablePeering {
		if err := (&peering.AcceptorController{
			Client:                   mgr.GetClient(),
			ConsulClientConfig:       consulConfig,
			ConsulServerConnMgr:      watcher,
			ExposeServersServiceName: c.flagResourcePrefix + "-expose-servers",
			ReleaseNamespace:         c.flagReleaseNamespace,
			Log:                      ctrl.Log.WithName("controller").WithName("peering-acceptor"),
			Scheme:                   mgr.GetScheme(),
			Context:                  ctx,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "peering-acceptor")
			return err
		}
		if err := (&peering.PeeringDialerController{
			Client:              mgr.GetClient(),
			ConsulClientConfig:  consulConfig,
			ConsulServerConnMgr: watcher,
			Log:                 ctrl.Log.WithName("controller").WithName("peering-dialer"),
			Scheme:              mgr.GetScheme(),
			Context:             ctx,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "peering-dialer")
			return err
		}

		mgr.GetWebhookServer().Register("/mutate-v1alpha1-peeringacceptors",
			&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.PeeringAcceptorWebhook{
				Client: mgr.GetClient(),
				Logger: ctrl.Log.WithName("webhooks").WithName("peering-acceptor"),
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-peeringdialers",
			&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.PeeringDialerWebhook{
				Client: mgr.GetClient(),
				Logger: ctrl.Log.WithName("webhooks").WithName("peering-dialer"),
			}})
	}

	mgr.GetWebhookServer().CertDir = c.flagCertDir

	mgr.GetWebhookServer().Register("/mutate",
		&ctrlRuntimeWebhook.Admission{Handler: &webhook.MeshWebhook{
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

	consulMeta := apicommon.ConsulMeta{
		PartitionsEnabled:    c.flagEnablePartitions,
		Partition:            c.consul.Partition,
		NamespacesEnabled:    c.flagEnableNamespaces,
		DestinationNamespace: c.flagConsulDestinationNamespace,
		Mirroring:            c.flagEnableK8SNSMirroring,
		Prefix:               c.flagK8SNSMirroringPrefix,
	}

	// Note: The path here should be identical to the one on the kubebuilder
	// annotation in each webhook file.
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicedefaults",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ServiceDefaultsWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ServiceDefaults),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceresolver",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ServiceResolverWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ServiceResolver),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-proxydefaults",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ProxyDefaultsWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ProxyDefaults),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-mesh",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.MeshWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.Mesh),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-exportedservices",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ExportedServicesWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ExportedServices),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicerouter",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ServiceRouterWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ServiceRouter),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicesplitter",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ServiceSplitterWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ServiceSplitter),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceintentions",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ServiceIntentionsWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ServiceIntentions),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-ingressgateway",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.IngressGatewayWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.IngressGateway),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-terminatinggateway",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.TerminatingGatewayWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.TerminatingGateway),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-samenessgroup",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.SamenessGroupWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.SamenessGroup),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-jwtprovider",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.JWTProviderWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.JWTProvider),
			ConsulMeta: consulMeta,
		}})
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-controlplanerequestlimits",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.ControlPlaneRequestLimitWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.ControlPlaneRequestLimit),
			ConsulMeta: consulMeta,
		}})

	mgr.GetWebhookServer().Register("/validate-v1alpha1-gatewaypolicy",
		&ctrlRuntimeWebhook.Admission{Handler: &v1alpha1.GatewayPolicyWebhook{
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("webhooks").WithName(apicommon.GatewayPolicy),
			ConsulMeta: consulMeta,
		}})

	if c.flagEnableWebhookCAUpdate {
		err = c.updateWebhookCABundle(ctx)
		if err != nil {
			setupLog.Error(err, "problem getting CA Cert")
			return err
		}
	}

	return nil
}

func (c *Command) updateWebhookCABundle(ctx context.Context) error {
	webhookConfigName := fmt.Sprintf("%s-connect-injector", c.flagResourcePrefix)
	caPath := fmt.Sprintf("%s/%s", c.flagCertDir, WebhookCAFilename)
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return err
	}
	err = webhookconfiguration.UpdateWithCABundle(ctx, c.clientset, webhookConfigName, caCert)
	if err != nil {
		return err
	}
	return nil
}
