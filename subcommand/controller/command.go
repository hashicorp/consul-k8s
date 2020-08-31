package controller

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/controllers"
	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	once sync.Once
	help string
	cert atomic.Value
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	tlsCertDir  = "/etc/controller-webhook/certs"
	tlsCertFile = "/etc/controller-webhook/certs/tls.crt"
	tlsKeyFile  = "/etc/controller-webhook/certs/tls.key"
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

func (c *Command) Run(_ []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(nil); err != nil {
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

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

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
	go c.certWatcher(ctx, certCh, mgr.GetClient())

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
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	return 0
}

func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.Bundle, clientset client.Client) {
	var bundle cert.Bundle
	for {
		select {
		case bundle = <-ch:
			//c.Output("Updated certificate bundle received. Updating certs...")
			// Bundle is updated, set it up

		case <-time.After(1 * time.Second):
			// This forces the mutating ctrlWebhook config to remain updated
			// fairly quickly. This is a jank way to do this and we should
			// look to improve it in the future. Since we use Patch requests
			// it is pretty cheap to do, though.

		case <-ctx.Done():
			// Quit
			return
		}

		webhookCert, err := tls.X509KeyPair(bundle.Cert, bundle.Key)
		if err != nil {
			//c.UI.Error(fmt.Sprintf("Error loading TLS keypair: %s", err))
			continue
		}

		// If there is a MWC name set, then update the CA bundle.
		if c.flagAutoName != "" && len(bundle.CACert) > 0 {
			ctrlWebhook := v1beta1.MutatingWebhookConfiguration{}
			err = clientset.Get(ctx, types.NamespacedName{Namespace: "", Name: c.flagAutoName}, &ctrlWebhook)
			if err != nil {
				//exit
				continue
			}

			// The CA Bundle value must be base64 encoded
			value := base64.StdEncoding.EncodeToString(bundle.CACert)

			var patches []string
			for i, _ := range ctrlWebhook.Webhooks {
				patches = append(patches, fmt.Sprintf(
					`[{
						"op": "add",
						"path": "/webhooks/%q/clientConfig/caBundle",
						"value": %q
					}]`, i, value))
			}
			webhookPatch := strings.Join(patches, ",")

			err := clientset.Patch(ctx, &ctrlWebhook, client.RawPatch(types.MergePatchType, []byte(webhookPatch)))
			if err != nil {
				//c.UI.Error(fmt.Sprintf(
				//	"Error updating MutatingWebhookConfiguration: %s",
				//	err))
				continue
			}
		}

		//Write certs to disk
		err = ioutil.WriteFile(tlsCertFile, bundle.Cert, os.ModePerm)
		if err != nil {
			continue
		}
		err = ioutil.WriteFile(tlsKeyFile, bundle.Key, os.ModePerm)
		if err != nil {
			continue
		}

		// Update the certificate
		c.cert.Store(&webhookCert)
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
