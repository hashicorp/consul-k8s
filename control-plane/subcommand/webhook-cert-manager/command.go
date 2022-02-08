package webhookcertmanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-multierror"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultCertExpiry    = 24 * time.Hour
	defaultRetryDuration = 1 * time.Second
)

type Command struct {
	UI cli.Ui

	flagSet *flag.FlagSet
	k8s     *flags.K8SFlags

	flagConfigFile string
	flagLogLevel   string
	flagLogJSON    bool

	flagDeploymentName      string
	flagDeploymentNamespace string

	clientset kubernetes.Interface

	once   sync.Once
	help   string
	sigCh  chan os.Signal
	logger hclog.Logger

	certExpiry *time.Duration // override default cert expiry of 24 hours if set (only set in tests)
	source     cert.Source    // override default cert source of cert.GenSource if set (only in tests)
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagConfigFile, "config-file", "",
		"Path to a config file to read webhook configs from. This file must be in JSON format.")
	c.flagSet.StringVar(&c.flagDeploymentName, "deployment-name", "",
		"Name of deployment that the cert-manager pod is managed by.")
	c.flagSet.StringVar(&c.flagDeploymentNamespace, "deployment-namespace", "",
		"Namespace of deployment that the cert-manager pod is managed by.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flagSet, c.k8s.Flags())
	c.help = flags.Usage(help, c.flagSet)

	// Wait on an interrupt or terminate to exit. This channel must be initialized before
	// Run() is called so that there are no race conditions where the channel
	// is not defined.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, syscall.SIGINT, syscall.SIGTERM)
	}
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("Error parsing flagSet: %s", err))
		return 1
	}
	if len(c.flagSet.Args()) > 0 {
		c.UI.Error("Invalid arguments: should have no non-flag arguments")
		return 1
	}

	if c.flagConfigFile == "" {
		c.UI.Error("-config-file must be set")
		return 1
	}

	if c.flagDeploymentName == "" {
		c.UI.Error("-deployment-name must be set")
		return 1
	}

	if c.flagDeploymentNamespace == "" {
		c.UI.Error("-deployment-namespace must be set")
		return 1
	}

	// Create the Kubernetes clientset
	if c.clientset == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}
		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	if c.logger == nil {
		var err error
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	configFile, err := ioutil.ReadFile(c.flagConfigFile)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error reading config file from %s: %s", c.flagConfigFile, err))
		return 1
	}
	var configs []webhookConfig
	err = json.Unmarshal(configFile, &configs)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error unmarshalling config file: %s", err.Error()))
		return 1
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	for i, config := range configs {
		if err := config.validate(ctx, c.clientset); err != nil {
			c.UI.Error(fmt.Sprintf("Error parsing config at index %d: %s", i, err))
			return 1
		}
	}

	// Create the certificate notifier so we can update certificates,
	// then start all the background routines for updating certificates.
	var notifiers []*cert.Notify
	var expiry time.Duration
	if c.certExpiry != nil {
		expiry = *c.certExpiry
	} else {
		expiry = defaultCertExpiry
	}
	var certSource cert.Source
	for _, config := range configs {
		if c.source != nil {
			certSource = c.source
		} else {
			certSource = &cert.GenSource{
				Name:   "Consul Webhook Certificates",
				Hosts:  config.TLSAutoHosts,
				Expiry: expiry,
			}
		}

		certCh := make(chan cert.MetaBundle)
		certNotify := &cert.Notify{Source: certSource, Ch: certCh, WebhookConfigName: config.Name, SecretName: config.SecretName, SecretNamespace: config.SecretNamespace}
		notifiers = append(notifiers, certNotify)
		go certNotify.Start(ctx)
		go c.certWatcher(ctx, certCh, c.clientset, c.logger)
	}

	// We define a signal handler for OS interrupts, and when an SIGINT or SIGTERM is received,
	// we gracefully shut down, by first stopping our cert notifiers and then cancelling
	// all the contexts that have been created by the process.
	sig := <-c.sigCh
	c.logger.Info(fmt.Sprintf("%s received, shutting down", sig))
	cancelFunc()
	for _, notifier := range notifiers {
		notifier.Stop()
	}
	return 0
}

// certWatcher listens for a new MetaBundle on the ch channel for all webhooks and updates
// MutatingWebhooksConfigs and Secrets when a new Bundle is available on the channel.
func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.MetaBundle, clientset kubernetes.Interface, log hclog.Logger) {
	var bundle cert.MetaBundle
	for {
		select {
		case bundle = <-ch:
			log.Info(fmt.Sprintf("Updated certificate bundle received for %s; Updating webhook certs.", bundle.WebhookConfigName))
			// Bundle is updated, set it up

		case <-time.After(defaultRetryDuration):
			// This forces the mutating ctrlWebhook config to remain updated
			// fairly quickly. Helm upgrades will rewrite the contents of the
			// CA bundle which will not be in sync with the certs in the system.
			// This fast reconcile ensures the system recovers fairly quickly in case
			// the secret or MWC gets deleted or reset.

		case <-ctx.Done():
			// Quit
			return
		}

		if err := c.reconcileCertificates(ctx, clientset, bundle, log); err != nil {
			log.Error("failed to reconcile certificates", "err", err)
		}
	}
}

// reconcileCertificates ensures the secret in the MetaBundle has the latest certificate from the MetaBundle and the caBundles on the
// MutatingWebhookConfiguration have the latest CA certificate from the MetaBundle. It updates them if they are outdated and exits early
// if they are up-to date.
func (c *Command) reconcileCertificates(ctx context.Context, clientset kubernetes.Interface, bundle cert.MetaBundle, log hclog.Logger) error {
	iterLog := log.With("mutatingwebhookconfig", bundle.WebhookConfigName, "secret", bundle.SecretName, "secretNS", bundle.SecretNamespace)

	deployment, err := clientset.AppsV1().Deployments(c.flagDeploymentNamespace).Get(ctx, c.flagDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	certSecret, err := clientset.CoreV1().Secrets(bundle.SecretNamespace).Get(ctx, bundle.SecretName, metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: bundle.SecretName,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deployment.Name,
						UID:        deployment.UID,
					},
				},
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				corev1.TLSCertKey:       bundle.Cert,
				corev1.TLSPrivateKeyKey: bundle.Key,
			},
			Type: corev1.SecretTypeTLS,
		}

		iterLog.Info("Creating Kubernetes secret with certificate")
		if _, err = clientset.CoreV1().Secrets(bundle.SecretNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			iterLog.Error(fmt.Sprintf("Error writing secret to API server: %s", err))
			return err
		}

		iterLog.Info("Updating webhook configuration")
		if err = c.updateWebhookConfig(ctx, bundle, clientset); err != nil {
			iterLog.Error("Error updating webhook configuration")
			return err
		}
		return nil
	} else if err != nil {
		iterLog.Error("getting secret from Kubernetes", "err", err)
		return err
	}

	// Don't update secret if the certificate and key are unchanged.
	if bytes.Equal(certSecret.Data[corev1.TLSCertKey], bundle.Cert) && bytes.Equal(certSecret.Data[corev1.TLSPrivateKeyKey], bundle.Key) && c.webhookUpdated(ctx, bundle, clientset) {
		return nil
	}

	if certSecret.ObjectMeta.Labels == nil {
		certSecret.ObjectMeta.Labels = map[string]string{common.CLILabelKey: common.CLILabelValue}
	} else {
		certSecret.ObjectMeta.Labels[common.CLILabelKey] = common.CLILabelValue
	}

	certSecret.Data[corev1.TLSCertKey] = bundle.Cert
	certSecret.Data[corev1.TLSPrivateKeyKey] = bundle.Key
	// Update the Owner Reference on an existing secret in case the secret
	// already existed in the cluster and was not created by this job.
	certSecret.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       deployment.Name,
			UID:        deployment.UID,
		},
	}

	iterLog.Info("Updating secret with new certificate")
	_, err = clientset.CoreV1().Secrets(bundle.SecretNamespace).Update(ctx, certSecret, metav1.UpdateOptions{})
	if err != nil {
		iterLog.Error("Error updating secret with certificate", "err", err)
		return err
	}

	iterLog.Info("Updating webhook configuration with new CA")
	if err = c.updateWebhookConfig(ctx, bundle, clientset); err != nil {
		iterLog.Error("Error updating webhook configuration", "err", err)
		return err
	}
	return nil
}

// updateWebhookConfig iterates over every webhook on the specified webhook configuration and updates
// their caBundle with the CA from the MetaBundle.
func (c *Command) updateWebhookConfig(ctx context.Context, metaBundle cert.MetaBundle, clientset kubernetes.Interface) error {
	if len(metaBundle.CACert) == 0 {
		return errors.New("no CA certificate in the bundle")
	}
	value := base64.StdEncoding.EncodeToString(metaBundle.CACert)

	webhookCfg, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, metaBundle.WebhookConfigName, metav1.GetOptions{})
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

	if _, err = clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Patch(ctx, metaBundle.WebhookConfigName, types.JSONPatchType, patchesJson, metav1.PatchOptions{}); err != nil {
		return err
	}
	return nil
}

// webhookUpdated verifies if every caBundle on the specified webhook configuration matches the desired CA certificate.
// It returns true if the CA is up-to date and false if it needs to be updated.
func (c *Command) webhookUpdated(ctx context.Context, bundle cert.MetaBundle, clientset kubernetes.Interface) bool {
	webhookCfg, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, bundle.WebhookConfigName, metav1.GetOptions{})
	if err != nil {
		return false
	}
	for _, webhook := range webhookCfg.Webhooks {
		if !bytes.Equal(webhook.ClientConfig.CABundle, bundle.CACert) {
			return false
		}
	}
	return true
}

type webhookConfig struct {
	Name            string   `json:"name,omitempty"`
	TLSAutoHosts    []string `json:"tlsAutoHosts,omitempty"`
	SecretName      string   `json:"secretName,omitempty"`
	SecretNamespace string   `json:"secretNamespace,omitempty"`
}

func (c webhookConfig) validate(ctx context.Context, client kubernetes.Interface) error {
	var err *multierror.Error
	if c.Name == "" {
		err = multierror.Append(err, errors.New(`config.Name cannot be ""`))
	} else {
		if _, err2 := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, c.Name, metav1.GetOptions{}); err2 != nil && k8serrors.IsNotFound(err2) {
			err = multierror.Append(err, fmt.Errorf("MutatingWebhookConfiguration with name \"%s\" must exist in cluster", c.Name))
		}
	}
	if c.SecretName == "" {
		err = multierror.Append(err, errors.New(`config.SecretName cannot be ""`))
	}
	if c.SecretNamespace == "" {
		err = multierror.Append(err, errors.New(`config.SecretNameSpace cannot be ""`))
	}

	if err != nil {
		err.ErrorFormat = func(errs []error) string {
			var errStr []string
			for _, e := range errs {
				errStr = append(errStr, e.Error())
			}
			return strings.Join(errStr, ", ")
		}
		return err
	}
	return nil
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
	c.sendSignal(syscall.SIGINT)
}

func (c *Command) sendSignal(sig os.Signal) {
	c.sigCh <- sig
}

const synopsis = "Starts the Consul Kubernetes webhook-cert-manager"
const help = `
Usage: consul-k8s-control-plane webhook-cert-manager [options]

  Starts the Consul Kubernetes webhook-cert-manager that manages the lifecycle for webhook TLS certificates.

`
