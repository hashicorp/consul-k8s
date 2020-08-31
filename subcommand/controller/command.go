package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/controllers"
	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type Command struct {
	flagSet   *flag.FlagSet
	k8s       *flags.K8SFlags
	httpFlags *flags.HTTPFlags

	flagMetricsAddr          string
	flagEnableLeaderElection bool
	flagAutoName             string // MutatingWebhookConfiguration for updating
	flagAutoHosts            string // SANs for the auto-generated TLS cert.
	flagCertFile             string // TLS cert for listening (PEM)
	flagKeyFile              string // TLS cert private key (PEM)

	clientset kubernetes.Interface

	once sync.Once
	help string
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	tlsCertDir  = "/tmp/controller-webhook/certs"
	tlsCertFile = "tls.crt"
	tlsKeyFile  = "tls.key"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagMetricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	c.flagSet.BoolVar(&c.flagEnableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller. "+
			"Enabling this will ensure there is only one active controller manager.")
	c.flagSet.StringVar(&c.flagAutoName, "tls-auto", "",
		"MutatingWebhookConfiguration name. If specified, will auto generate cert bundle.")
	c.flagSet.StringVar(&c.flagAutoHosts, "tls-auto-hosts", "",
		"Comma-separated hosts for auto-generated TLS cert. If specified, will auto generate cert bundle.")
	c.flagSet.StringVar(&c.flagCertFile, "tls-cert-file", "",
		"PEM-encoded TLS certificate to serve. If blank, will generate random cert.")
	c.flagSet.StringVar(&c.flagKeyFile, "tls-key-file", "",
		"PEM-encoded TLS private key to serve. If blank, will generate random cert.")

	c.httpFlags = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.httpFlags.Flags())
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		setupLog.Error(err, "parsing flagSet")
		return 1
	}
	if len(c.flagSet.Args()) > 0 {
		setupLog.Error(errors.New("should have no non-flag arguments"), "invalid arguments")
		return 1
	}

	var certSource cert.Source = &cert.GenSource{
		Name:  "Consul Controller",
		Hosts: strings.Split(c.flagAutoHosts, ","),
	}
	if c.flagCertFile != "" {
		certSource = &cert.DiskSource{
			CertPath: c.flagCertFile,
			KeyPath:  c.flagKeyFile,
		}
	}

	if c.clientset == nil {
		config, err := rest.InClusterConfig()
		if err != nil {
			setupLog.Error(err, "Error loading in-cluster K8S config")
			return 1
		}
		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			setupLog.Error(err, "Error creating K8S client")
			return 1
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: c.flagMetricsAddr,
		Port:               9443,
		LeaderElection:     c.flagEnableLeaderElection,
		LeaderElectionID:   "consul.hashicorp.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return 1
	}

	// Create the certificate notifier so we can update for certificates,
	// then start all the background routines for updating certificates.
	certCh := make(chan cert.Bundle)
	certNotify := &cert.Notify{Ch: certCh, Source: certSource}
	defer certNotify.Stop()
	go certNotify.Start(context.Background())
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	go c.certWatcher(ctx, certCh, c.clientset, setupLog)

	consulClient, err := c.httpFlags.APIClient()
	if err != nil {
		setupLog.Error(err, "connecting to Consul agent")
		return 1
	}

	if err = (&controllers.ServiceDefaultsReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("ServiceDefaults"),
		Scheme:       mgr.GetScheme(),
		ConsulClient: consulClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceDefaults")
		return 1
	}

	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		mgr.GetWebhookServer().CertDir = tlsCertDir
		//Note: The path here should be identical to the one on the kubebuilder annotation in file api/v1alpha1/servicedefaults_webhook.go
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicedefaults",
			&webhook.Admission{Handler: v1alpha1.NewServiceDefaultsValidator(mgr.GetClient(), consulClient, ctrl.Log.WithName("webhooks").WithName("ServiceDefaults"))})
	}

	err = waitForCerts()
	if err != nil {
		setupLog.Error(err, "Error reading certificate files")
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	return 0
}

func waitForCerts() error {
	for {
		_, err := os.Stat(filepath.Join(tlsCertDir, tlsCertFile))
		if err == nil {
			return nil
		}
		if os.IsNotExist(err) {
			time.Sleep(1 * time.Millisecond)
			continue
		} else {
			return err
		}
	}
}

func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.Bundle, clientset kubernetes.Interface, log logr.Logger) {
	var bundle cert.Bundle
	for {
		select {
		case bundle = <-ch:
			log.Info("Updated certificate bundle received. Updating webhook certs.")
			// Bundle is updated, set it up

		case <-time.After(30 * time.Minute):
			// This forces the mutating ctrlWebhook config to remain updated
			// fairly quickly. This is a jank way to do this and we should
			// look to improve it in the future. Since we use Patch requests
			// it is pretty cheap to do, though.

		case <-ctx.Done():
			// Quit
			return
		}

		if _, err := os.Stat(tlsCertDir); os.IsNotExist(err) {
			err = os.MkdirAll(tlsCertDir, os.ModePerm)
			if err != nil {
				log.Error(err, "Creating cert folder")
				continue
			}
		}

		if err := ioutil.WriteFile(filepath.Join(tlsCertDir, tlsCertFile), bundle.Cert, os.ModePerm); err != nil {
			log.Error(err, "Error writing TLS cert to disk")
			continue
		}
		if err := ioutil.WriteFile(filepath.Join(tlsCertDir, tlsKeyFile), bundle.Key, os.ModePerm); err != nil {
			log.Error(err, "Error writing TLS key to disk")
			continue
		}

		// If there is a MWC name set, then update the CA bundle.
		if c.flagAutoName != "" && len(bundle.CACert) > 0 {

			value := base64.StdEncoding.EncodeToString(bundle.CACert)

			webhookCfg, err := clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(ctx, c.flagAutoName, metav1.GetOptions{})
			if err != nil {
				log.Error(err, "Error retrieving MutatingWebhookConfiguration from API")
				continue
			}
			var patches []string
			for i := range webhookCfg.Webhooks {
				patches = append(patches, fmt.Sprintf(
					`{
						"op": "add",
						"path": "/webhooks/%d/clientConfig/caBundle",
						"value": %q
					}`, i, value))
			}
			webhookPatch := fmt.Sprintf("[%s]", strings.Join(patches, ","))

			if _, err = clientset.AdmissionregistrationV1beta1().
				MutatingWebhookConfigurations().
				Patch(ctx, c.flagAutoName, types.JSONPatchType, []byte(webhookPatch), metav1.PatchOptions{}); err != nil {
				log.Error(err, "Error updating MutatingWebhookConfiguration")
				continue
			}
		}
	}
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) Synopsis() string {
	return synopsis
}

const synopsis = "Starts the Consul Kubernetes controller"
const help = `
Usage: consul-k8s controller [options]

  Starts the Consul Kubernetes controller that manages Consul Custom Resource Definitions

`
