package certmanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Command struct {
	flagSet   *flag.FlagSet
	httpFlags *flags.HTTPFlags
	UI        cli.Ui

	flagWebhookName     string // MutatingWebhookConfiguration for updating
	flagAutoHosts       string // SANs for the auto-generated TLS cert.
	flagSecretName      string // Name of secret where certificates will be written to.
	flagSecretNamespace string // Namespace of the secret where certificates will be written to.
	flagCertFile        string // TLS cert for listening (PEM)
	flagKeyFile         string // TLS cert private key (PEM)

	clientset kubernetes.Interface

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagWebhookName, "webhook-name", "",
		"MutatingWebhookConfiguration name. If specified, will auto generate cert bundle.")
	c.flagSet.StringVar(&c.flagAutoHosts, "tls-hosts", "",
		"Comma-separated hosts for auto-generated TLS cert. If specified, will auto generate cert bundle.")
	c.flagSet.StringVar(&c.flagSecretName, "secret-name", "",
		"Name of the secret to update TLS certificates")
	c.flagSet.StringVar(&c.flagSecretNamespace, "secret-namespace", "default",
		"Namespace of the secret to update TLS certificates. Defaults to default")
	c.flagSet.StringVar(&c.flagCertFile, "tls-cert-file", "",
		"PEM-encoded TLS certificate to serve. If blank, will generate random cert.")
	c.flagSet.StringVar(&c.flagKeyFile, "tls-key-file", "",
		"PEM-encoded TLS private key to serve. If blank, will generate random cert.")

	c.httpFlags = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.httpFlags.Flags())
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("parsing flagSet: %s", err))
		return 1
	}
	if len(c.flagSet.Args()) > 0 {
		c.UI.Error(fmt.Sprintf("invalid arguments: %s", errors.New("should have no non-flag arguments")))
		return 1
	}

	if c.flagSecretName == "" {
		c.UI.Error(fmt.Sprintf("invalid arguments: %s", errors.New("secret-name must be set")))
		return 1
	}

	if c.flagCertFile == "" && c.flagKeyFile == "" && c.flagWebhookName == "" {
		c.UI.Error(fmt.Sprintf("invalid arguments: %s", errors.New("either webhook-name or tls-cert-file and tls-key-file must be provided")))
		return 1
	}

	if (c.flagCertFile != "" && c.flagKeyFile == "") || (c.flagCertFile == "" && c.flagKeyFile != "") {
		c.UI.Error(fmt.Sprintf("invalid arguments: %s", errors.New("both tls-cert-file and tls-key-file must be provided")))
		return 1
	}

	if c.clientset == nil {
		config, err := rest.InClusterConfig()
		if err != nil {
			c.UI.Error(fmt.Sprintf("error loading in-cluster K8S config: %s", err))
			return 1
		}
		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("error creating K8S client: %s", err))
			return 1
		}
	}

	var certSource cert.Source = &cert.GenSource{
		Name:  "Consul Webhook Certificates",
		Hosts: strings.Split(c.flagAutoHosts, ","),
	}
	if c.flagCertFile != "" {
		certSource = &cert.DiskSource{
			CertPath: c.flagCertFile,
			KeyPath:  c.flagKeyFile,
		}
	}

	// Create the certificate notifier so we can update for certificates,
	// then start all the background routines for updating certificates.
	certCh := make(chan cert.Bundle)
	certNotify := &cert.Notify{Ch: certCh, Source: certSource}
	defer certNotify.Stop()
	go certNotify.Start(context.Background())
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	c.certWatcher(ctx, certCh, c.clientset, c.UI)
	return 0
}

func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.Bundle, clientset kubernetes.Interface, log cli.Ui) {
	var bundle cert.Bundle
	for {
		select {
		case bundle = <-ch:
			log.Info("updated certificate bundle received. updating webhook certs.")
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

		log.Info("getting secret from kubernetes")
		certSecret, err := clientset.CoreV1().Secrets(c.flagSecretNamespace).Get(ctx, c.flagSecretName, metav1.GetOptions{})
		if err != nil && k8serrors.IsNotFound(err) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      c.flagSecretName,
					Namespace: c.flagSecretNamespace,
				},
				Data: map[string][]byte{
					corev1.TLSCertKey:       bundle.Cert,
					corev1.TLSPrivateKeyKey: bundle.Key,
				},
				Type: corev1.SecretTypeTLS,
			}

			log.Info("creating kubernetes secret")
			_, err := clientset.CoreV1().Secrets(c.flagSecretNamespace).Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				log.Error(fmt.Sprintf("error writing secret to API server: %s", err))
				continue
			}

			log.Info("updating webhook configuration with new CA")
			err = c.updateWebhookConfig(bundle, clientset, ctx)
			if err != nil {
				log.Error(fmt.Sprintf("error updating webhook configuration: %s", err))
				continue
			}
		} else if err != nil {
			log.Error(fmt.Sprintf("error getting secret for API server: %s", err))
			continue
		}

		//Dont update secret if the certificate is unchanged
		if bytes.Equal(certSecret.Data[corev1.TLSCertKey], bundle.Cert) {
			continue
		}

		certSecret.Data[corev1.TLSCertKey] = bundle.Cert
		certSecret.Data[corev1.TLSPrivateKeyKey] = bundle.Key

		log.Info("updating secret with new certificate")
		_, err = clientset.CoreV1().Secrets(c.flagSecretNamespace).Update(ctx, certSecret, metav1.UpdateOptions{})
		if err != nil {
			log.Error(fmt.Sprintf("error updating secret with certificate: %s", err))
			continue
		}

		log.Info("updating webhook configuration with new CA")
		err = c.updateWebhookConfig(bundle, clientset, ctx)
		if err != nil {
			log.Error(fmt.Sprintf("error updating webhook configuration: %s", err))
			continue
		}
	}
}

func (c *Command) updateWebhookConfig(bundle cert.Bundle, clientset kubernetes.Interface, ctx context.Context) error {
	if c.flagWebhookName != "" && len(bundle.CACert) > 0 {
		value := base64.StdEncoding.EncodeToString(bundle.CACert)

		// If there is a MWC name set, then update the CA bundle on all the webhooks on that MWC.
		webhookCfg, err := clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(ctx, c.flagWebhookName, metav1.GetOptions{})
		if err != nil {
			return err
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

		if _, err = clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Patch(ctx, c.flagWebhookName, types.JSONPatchType, []byte(webhookPatch), metav1.PatchOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) Synopsis() string {
	return synopsis
}

const synopsis = "Starts the Consul Kubernetes cert-manager"
const help = `
Usage: consul-k8s cert-manager [options]

  Starts the Consul Kubernetes cert-manager that manages the lifecycle for webhook TLS certificates

`
