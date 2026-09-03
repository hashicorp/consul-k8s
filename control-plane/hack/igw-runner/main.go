// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
//
// igw-runner — LOCAL DEV ONLY. Not part of the production binary.
//
// The InferenceGatewayController is fully integrated into the production
// inject-connect command via v1controllers.go. Use this runner only for
// rapid local iteration without standing up the full webhook/cert machinery.
//
// The production path is:
//
//	inject-connect -enable-ai -ai-inference-gateway-image=<image> ...
//
// This file is excluded from normal builds via the "ignore" build tag.
// To run it:
//
//	go run -tags ignore ./hack/igw-runner \
//	  -consul-http-addr=127.0.0.1:8500 \
//	  -consul-grpc-port=8502 \
//	  -datacenter=dc1 \
//	  -gateway-image=consul-inference-gateway:0.1.0-dev

//go:build ignore

package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	consulapi "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	aicontrollers "github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/ai"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(consulapi.AddToScheme(scheme))
}

func main() {
	var (
		flagConsulHTTPAddr  = flag.String("consul-http-addr", "127.0.0.1:8500", "Consul HTTP address (host:port)")
		flagConsulGRPCPort  = flag.Int("consul-grpc-port", 8502, "Consul gRPC port")
		flagDatacenter      = flag.String("datacenter", "dc1", "Consul datacenter")
		flagGatewayImage    = flag.String("gateway-image", "placeholder:latest", "Container image for the inference-gateway binary")
		flagKubeconfigPath  = flag.String("kube-config-path", "", "Path to kubeconfig (defaults to KUBECONFIG env or ~/.kube/config)")
		flagConsulNamespace = flag.String("consul-namespace", "", "Consul namespace (enterprise only)")
		flagConsulPartition = flag.String("consul-partition", "", "Consul partition (enterprise only)")
	)
	flag.Parse()

	// ── Logger ──────────────────────────────────────────────────────────────
	opts := zap.Options{Development: true}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("igw-runner")

	log.Info("starting",
		"consulHTTPAddr", *flagConsulHTTPAddr,
		"datacenter", *flagDatacenter,
		"gatewayImage", *flagGatewayImage,
	)

	// ── K8s config ──────────────────────────────────────────────────────────
	kubeconfig := *flagKubeconfigPath
	if kubeconfig == "" {
		if kc := os.Getenv("KUBECONFIG"); kc != "" {
			kubeconfig = kc
		} else {
			home, _ := os.UserHomeDir()
			kubeconfig = home + "/.kube/config"
		}
	}
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Error(err, "failed to load kubeconfig", "path", kubeconfig)
		os.Exit(1)
	}

	// ── Consul config ───────────────────────────────────────────────────────
	host, portStr, err := net.SplitHostPort(*flagConsulHTTPAddr)
	if err != nil {
		log.Error(err, "invalid -consul-http-addr", "addr", *flagConsulHTTPAddr)
		os.Exit(1)
	}
	httpPort, _ := strconv.Atoi(portStr)

	consulCfg := &consul.Config{
		APIClientConfig: &capi.Config{Address: *flagConsulHTTPAddr},
		HTTPPort:        httpPort,
		GRPCPort:        *flagConsulGRPCPort,
		APITimeout:      10 * time.Second,
	}

	// ── Consul connection manager ───────────────────────────────────────────
	connMgrCfg := discovery.Config{
		Addresses: host,
		GRPCPort:  *flagConsulGRPCPort,
	}
	hcLogger := hclog.New(&hclog.LoggerOptions{
		Name:  "consul-connmgr",
		Level: hclog.Info,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	watcher, err := discovery.NewWatcher(ctx, connMgrCfg, hcLogger)
	if err != nil {
		log.Error(err, "failed to create Consul watcher")
		os.Exit(1)
	}
	go watcher.Run()
	defer watcher.Stop()

	log.Info("waiting for Consul connection...")
	if _, err := watcher.State(); err != nil {
		log.Error(err, "Consul watcher failed to initialise")
		os.Exit(1)
	}
	log.Info("Consul connected ✓", "addr", *flagConsulHTTPAddr)

	// ── Controller manager (no webhook) ─────────────────────────────────────
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // disabled
		},
		HealthProbeBindAddress: "0", // disabled
		LeaderElection:         false,
	})
	if err != nil {
		log.Error(err, "failed to create manager")
		os.Exit(1)
	}

	// ── Register InferenceGateway controller ────────────────────────────────
	igwCtrl := &aicontrollers.InferenceGatewayController{
		Client:       mgr.GetClient(),
		Log:          log.WithName("InferenceGatewayController"),
		Recorder:     mgr.GetEventRecorderFor("inferencegateway-controller"),
		GatewayImage: *flagGatewayImage,
		ConsulClientConfig:   consulCfg,
		ConsulServerConnMgr:  watcher,
		ConsulPartition:      *flagConsulPartition,
		ConsulNamespace:      *flagConsulNamespace,
		Datacenter:           *flagDatacenter,
	}
	if err := igwCtrl.SetupWithManager(ctx, mgr); err != nil {
		log.Error(err, "failed to setup InferenceGatewayController")
		os.Exit(1)
	}
	log.Info("InferenceGatewayController registered ✓")

	// ── Run ──────────────────────────────────────────────────────────────────
	log.Info("manager started — press Ctrl+C to stop")
	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "manager exited with error")
		os.Exit(1)
	}
	log.Info("igw-runner stopped")
}
