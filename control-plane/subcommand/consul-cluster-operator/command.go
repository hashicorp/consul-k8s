// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulclusteroperator

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mitchellh/cli"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	consulclustercontroller "github.com/hashicorp/consul-k8s/control-plane/controllers/consulcluster"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

// Command is the subcommand for running the ConsulCluster operator.
type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	flagLogLevel            string
	flagLogJSON             bool
	flagMetricsBindAddress  string
	flagHealthBindAddress   string
	flagLeaderElectionID    string
	flagLeaderElectionNS    string

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values: trace, debug, info, warn, error.")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable JSON output for logger.")
	c.flags.StringVar(&c.flagMetricsBindAddress, "metrics-bind-address", "0.0.0.0:9446",
		"Address to bind the metrics endpoint.")
	c.flags.StringVar(&c.flagHealthBindAddress, "health-probe-bind-address", "0.0.0.0:9447",
		"Address to bind the health probe endpoint.")
	c.flags.StringVar(&c.flagLeaderElectionID, "leader-election-id", "consul-cluster-operator-lock",
		"Name of the leader election lease.")
	c.flags.StringVar(&c.flagLeaderElectionNS, "leader-election-namespace", "",
		"Namespace for leader election. Defaults to the operator's own namespace.")

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())

	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		c.UI.Error(fmt.Sprintf("Failed to parse args: %v", err))
		return 1
	}

	level, err := zapcore.ParseLevel(c.flagLogLevel)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unknown log level %q: %s", c.flagLogLevel, err))
		return 1
	}

	zapLogger := zap.New(zap.UseDevMode(level == zapcore.DebugLevel || level == zapcore.InvalidLevel),
		zap.Level(level),
		zap.JSONEncoder(func(cfg *zapcore.EncoderConfig) {
			cfg.EncodeTime = zapcore.ISO8601TimeEncoder
		}))
	if !c.flagLogJSON {
		zapLogger = zap.New(zap.UseDevMode(false), zap.Level(level))
	}
	ctrl.SetLogger(zapLogger)

	cfg := ctrl.GetConfigOrDie()
	cfg.Timeout = 90 * time.Second
	cfg.QPS = 50
	cfg.Burst = 100

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  scheme,
		LeaderElection:          true,
		LeaderElectionID:        c.flagLeaderElectionID,
		LeaderElectionNamespace: c.flagLeaderElectionNS,
		Logger:                  zapLogger,
		LeaseDuration:           ptr.To(90 * time.Second),
		RenewDeadline:           ptr.To(60 * time.Second),
		RetryPeriod:             ptr.To(15 * time.Second),
		Metrics: metricsserver.Options{
			BindAddress: c.flagMetricsBindAddress,
		},
		HealthProbeBindAddress: c.flagHealthBindAddress,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return 1
	}

	if err := (&consulclustercontroller.ConsulClusterReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controller").WithName("ConsulCluster"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ConsulCluster")
		return 1
	}

	if err := mgr.AddHealthzCheck("healthz", func(_ *http.Request) error { return nil }); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return 1
	}
	if err := mgr.AddReadyzCheck("readyz", func(_ *http.Request) error { return nil }); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	setupLog.Info("starting consul-cluster-operator")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	return 0
}

func (c *Command) Synopsis() string {
	return "Runs the ConsulCluster operator to provision Consul server clusters."
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const help = `
Usage: consul-k8s consul-cluster-operator [options]

  Runs the ConsulCluster controller-runtime operator. Watches ConsulCluster
  custom resources and provisions Consul server pods and PVCs.

`
