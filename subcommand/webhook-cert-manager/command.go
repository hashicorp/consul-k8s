package webhookcertmanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul-k8s/subcommand"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flagSet   *flag.FlagSet
	httpFlags *flags.HTTPFlags
	k8s       *flags.K8SFlags

	flagConfigFile string

	clientset kubernetes.Interface

	once  sync.Once
	help  string
	sigCh chan os.Signal
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagConfigFile, "config-file", "",
		"Path to config file to read webhook configs from")

	c.httpFlags = &flags.HTTPFlags{}
	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flagSet, c.httpFlags.Flags())
	flags.Merge(c.flagSet, c.k8s.Flags())
	c.help = flags.Usage(help, c.flagSet)

	// Wait on an interrupt to exit. This channel must be initialized before
	// Run() is called so that there are no race conditions where the channel
	// is not defined.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, os.Interrupt)
	}
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("parsing flagSet: %s", err))
		return 1
	}
	if len(c.flagSet.Args()) > 0 {
		c.UI.Error("invalid arguments: should have no non-flag arguments")
		return 1
	}

	if c.flagConfigFile == "" {
		c.UI.Error(fmt.Sprintf("-config-file must be set"))
		return 1
	}

	// Create the Kubernetes clientset
	if c.clientset == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("error retrieving Kubernetes auth: %s", err))
			return 1
		}
		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	configFile, err := ioutil.ReadFile(c.flagConfigFile)
	if err != nil {
		c.UI.Error(fmt.Sprintf("error reading config file from %s", c.flagConfigFile))
	}
	var configs []webhookConfig
	err = json.Unmarshal(configFile, &configs)
	if err != nil {
		c.UI.Error(fmt.Sprintf("error unmarshalling config file: %s", err.Error()))
	}

	// Create the certificate notifier so we can update for certificates,
	// then start all the background routines for updating certificates.
	ctx := context.Background()
	ctx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	certCh := make(chan cert.MetaBundle)

	var notifiers []*cert.Notify

	for _, config := range configs {
		certSource := &cert.GenSource{
			Name:  "Consul Webhook Certificates",
			Hosts: config.TLSAutoHosts,
		}
		certNotify := &cert.Notify{Source: certSource, Ch: certCh, WebhookName: config.Name, SecretName: config.SecretName, SecretNamespace: config.SecretNamespace}
		notifiers = append(notifiers, certNotify)
		go certNotify.Start(ctx)
	}

	go c.certWatcher(ctx, certCh, c.clientset, c.UI)

	closeNotifiers := func() {
		for _, notifier := range notifiers {
			notifier.Stop()
		}
	}
	defer closeNotifiers()

	// Interrupted, gracefully exit
	select {
	case <-c.sigCh:
		closeNotifiers()
		cancelFunc()
		return 0
	}
}

func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.MetaBundle, clientset kubernetes.Interface, log cli.Ui) {
	var bundle cert.MetaBundle
	for {
		select {
		case bundle = <-ch:
			log.Info("updated certificate bundle received. updating webhook certs.")
			// Bundle is updated, set it up

		case <-time.After(30 * time.Minute):
			// This forces the mutating ctrlWebhook config to remain updated
			// fairly quickly. This is done every 30 minutes to ensure the certificates
			// are in sync. Because the certificate and key are being read from a secret,
			// this does not have to be processed as aggressively as the 1 sec time in
			// the connect inject cert watcher.

		case <-ctx.Done():
			// Quit
			return
		}

		log.Info("getting secret from kubernetes")
		certSecret, err := clientset.CoreV1().Secrets(bundle.SecretNamespace).Get(ctx, bundle.SecretName, metav1.GetOptions{})
		if err != nil && k8serrors.IsNotFound(err) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bundle.SecretName,
					Namespace: bundle.SecretNamespace,
				},
				Data: map[string][]byte{
					corev1.TLSCertKey:       bundle.Cert,
					corev1.TLSPrivateKeyKey: bundle.Key,
				},
				Type: corev1.SecretTypeTLS,
			}

			log.Info("creating kubernetes secret")
			if _, err := clientset.CoreV1().Secrets(bundle.SecretNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
				log.Error(fmt.Sprintf("error writing secret to API server: %s", err))
				continue
			}

			log.Info("updating webhook configuration with new CA")
			if err := c.updateWebhookConfig(ctx, bundle, clientset); err != nil {
				log.Error(fmt.Sprintf("error updating webhook configuration: %s", err))
				continue
			}
			continue
		} else if err != nil {
			log.Error(fmt.Sprintf("error getting secret for API server: %s", err))
			continue
		}

		// Don't update secret if the certificate is unchanged.
		if bytes.Equal(certSecret.Data[corev1.TLSCertKey], bundle.Cert) {
			continue
		}

		certSecret.Data[corev1.TLSCertKey] = bundle.Cert
		certSecret.Data[corev1.TLSPrivateKeyKey] = bundle.Key

		log.Info("updating secret with new certificate")
		_, err = clientset.CoreV1().Secrets(bundle.SecretNamespace).Update(ctx, certSecret, metav1.UpdateOptions{})
		if err != nil {
			log.Error(fmt.Sprintf("error updating secret with certificate: %s", err))
			continue
		}

		log.Info("updating webhook configuration with new CA")
		if err := c.updateWebhookConfig(ctx, bundle, clientset); err != nil {
			log.Error(fmt.Sprintf("error updating webhook configuration: %s", err))
			continue
		}
	}
}

func (c *Command) updateWebhookConfig(ctx context.Context, metaBundle cert.MetaBundle, clientset kubernetes.Interface) error {
	if len(metaBundle.CACert) > 0 {
		value := base64.StdEncoding.EncodeToString(metaBundle.CACert)

		webhookCfg, err := clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(ctx, metaBundle.WebhookConfigName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		var patches []patch
		for i := range webhookCfg.Webhooks {
			patches = append(patches, patch{
				Op:    "add",
				Path:  fmt.Sprintf("/webhooks/%d/clientConfig/caBundle", i),
				Value: value,
			})
		}
		patchesJson, err := json.Marshal(patches)
		if err != nil {
			return err
		}

		if _, err = clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Patch(ctx, metaBundle.WebhookConfigName, types.JSONPatchType, patchesJson, metav1.PatchOptions{}); err != nil {
			return err
		}
	}
	return nil
}

type webhookConfig struct {
	Name            string   `json:"name,omitempty"`
	TLSAutoHosts    []string `json:"tlsAutoHosts,omitempty"`
	SecretName      string   `json:"secretName,omitempty"`
	SecretNamespace string   `json:"secretNamespace,omitempty"`
}

type patch struct {
	Op    string `json:"op,omitempty"`
	Path  string `json:"path,omitempty"`
	Value string `json:"value,omitempty"`
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) Synopsis() string {
	return synopsis
}

// interrupt sends os.Interrupt signal to the command
// so it can exit gracefully. This function is needed for tests
func (c *Command) interrupt() {
	c.sigCh <- os.Interrupt
}

const synopsis = "Starts the Consul Kubernetes webhook-cert-manager"
const help = `
Usage: consul-k8s webhook-cert-manager [options]

  Starts the Consul Kubernetes webhook-cert-manager that manages the lifecycle for webhook TLS certificates

`
