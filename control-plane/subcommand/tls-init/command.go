package tls_init

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI        cli.Ui
	clientset kubernetes.Interface

	flags    *flag.FlagSet
	k8sFlags *flags.K8SFlags

	// flags that support the CA/key as files on disk.
	flagCaFile  string
	flagKeyFile string

	// value that support the CA/key as secrets in Kubernetes.
	caCertSecret *corev1.Secret
	caKeySecret  *corev1.Secret

	// flags that dictate the specifications of the created certs.
	flagDays        int
	flagDomain      string
	flagDC          string
	flagDNSNames    flags.AppendSliceValue
	flagIPAddresses flags.AppendSliceValue

	// flags that dictate specifics for the secret name and namespace
	// that are created by the command.
	flagK8sNamespace string
	flagNamePrefix   string

	// log
	log          hclog.Logger
	flagLogLevel string
	flagLogJSON  bool

	ctx context.Context

	once sync.Once
	help string
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("Failed to parse args: %v", err))
		return 1
	}

	if len(c.flags.Args()) > 0 {
		c.UI.Error("Should have no non-flag arguments.")
		return 1
	}

	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	var err error
	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.clientset == nil {
		if err := c.configureKubeClient(); err != nil {
			c.UI.Error(fmt.Sprintf("error configuring kubernetes: %v", err))
			return 1
		}
	}

	var cancel context.CancelFunc
	c.ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Get CA cert and key from the Kubernetes secrets if they are not provided as files.
	if c.flagCaFile == "" && c.flagKeyFile == "" {
		c.caCertSecret, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, fmt.Sprintf("%s-ca-cert", c.flagNamePrefix), metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			c.UI.Error(fmt.Sprintf("error reading secret from Kubernetes: %v", err))
			return 1
		} else if err != nil {
			// Explicitly set value to nil if the secret isn't found
			// so that we can later determine whether to create a new CA.
			c.caCertSecret = nil
		}
		c.caKeySecret, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, fmt.Sprintf("%s-ca-key", c.flagNamePrefix), metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			c.UI.Error(fmt.Sprintf("error reading secret from Kubernetes: %v", err))
			return 1
		} else if err != nil {
			// Explicitly set value to nil if the secret isn't found
			// so that we can later determine whether to create a new CA.
			c.caKeySecret = nil
		}
	}

	var (
		ca string
		pk string
	)

	// Only create a CA certificate/key pair if it doesn't exist or hasn't been provided
	if !c.caExists() {
		c.log.Info("no existing CA found; generating new CA certificate and key")
		_, pk, ca, _, err = cert.GenerateCA("Consul Agent CA")
		if err != nil {
			c.log.Error("error generating Consul Agent CA certificate and private key", "err", err)
			return 1
		}

		c.log.Info("saving CA certificate", "secret", fmt.Sprintf("%s-ca-cert", c.flagNamePrefix))
		c.caCertSecret, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Create(c.ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-ca-cert", c.flagNamePrefix),
				Namespace: c.flagK8sNamespace,
			},
			Data: map[string][]byte{
				corev1.TLSCertKey: []byte(ca),
			},
			Type: corev1.SecretTypeOpaque,
		}, metav1.CreateOptions{})

		if err != nil {
			c.log.Error("error saving CA certificate secret to kubernetes", "err", err)
			return 1
		}
		c.log.Info("saving ca private key", "secret", fmt.Sprintf("%s-ca-key", c.flagNamePrefix))
		c.caKeySecret, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Create(c.ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-ca-key", c.flagNamePrefix),
				Namespace: c.flagK8sNamespace,
			},
			Data: map[string][]byte{
				corev1.TLSPrivateKeyKey: []byte(pk),
			},
			Type: corev1.SecretTypeOpaque,
		}, metav1.CreateOptions{})

		if err != nil {
			c.log.Error("error saving CA private key secret to kubernetes", "err", err)
			return 1
		}
		c.log.Info("successfully saved CA certificate and private key")
	} else {
		c.log.Info("using existing CA")
	}

	var hosts []string
	var name string
	var caBytes, keyBytes []byte

	if c.flagCaFile != "" && c.flagKeyFile != "" {
		c.log.Info("reading CA certificate from provided file")
		caBytes, err = ioutil.ReadFile(c.flagCaFile)
		if err != nil {
			c.log.Error("error reading provided CA file", "err", err)
			return 1
		}
		c.log.Info("reading CA private key from provided file")
		keyBytes, err = ioutil.ReadFile(c.flagKeyFile)
		if err != nil {
			c.log.Error("error reading provided private key file", "err", err)
			return 1
		}
	} else {
		// We assume that these secrets aren't nil becase
		// we created them above in case they are nil.
		caBytes = c.caCertSecret.Data[corev1.TLSCertKey]
		keyBytes = c.caKeySecret.Data[corev1.TLSPrivateKeyKey]
	}
	ca = string(caBytes)
	pk = string(keyBytes)

	for _, d := range c.flagDNSNames {
		if len(d) > 0 {
			hosts = append(hosts, strings.TrimSpace(d))
		}
	}

	for _, i := range c.flagIPAddresses {
		if len(i) > 0 {
			hosts = append(hosts, strings.TrimSpace(i))
		}
	}

	name = fmt.Sprintf("server.%s.%s", c.flagDC, c.flagDomain)
	hosts = append(hosts, name, "localhost", "127.0.0.1")

	c.log.Info("parsing certificate signer from CA private key")
	signer, err := cert.ParseSigner(pk)
	if err != nil {
		c.log.Error("error parsing signer from private key", "err", err)
		return 1
	}

	c.log.Info("parsing CA certificate from PEM string")
	caCert, err := cert.ParseCert([]byte(ca))
	if err != nil {
		c.log.Error("error parsing CA certificate from PEM string", "err", err)
		return 1
	}

	c.log.Info("generating server certificate and private key")
	serverCert, serverKey, err := cert.GenerateCert(name, c.getDaysAsDuration(), caCert, signer, hosts)
	if err != nil {
		c.log.Error("error generating server certificate and private key", "err", err)
		return 1
	}

	serverCertSecret, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, fmt.Sprintf("%s-server-cert", c.flagNamePrefix), metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		c.log.Info("creating server certificate and private key secret")
		_, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Create(c.ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: c.flagK8sNamespace,
				Name:      fmt.Sprintf("%s-server-cert", c.flagNamePrefix),
			},
			Data: map[string][]byte{
				corev1.TLSCertKey:       []byte(serverCert),
				corev1.TLSPrivateKeyKey: []byte(serverKey),
			},
			Type: corev1.SecretTypeTLS,
		}, metav1.CreateOptions{})
		if err != nil {
			c.log.Error("error creating server certificate secret in kubernetes", "err", err)
			return 1
		}
	} else if err == nil {
		serverCertSecret.Data = map[string][]byte{
			corev1.TLSCertKey:       []byte(serverCert),
			corev1.TLSPrivateKeyKey: []byte(serverKey),
		}
		c.log.Info("updating server certificate and private key secret")
		_, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Update(c.ctx, serverCertSecret, metav1.UpdateOptions{})
		if err != nil {
			c.log.Error("error updating server certificate secret in kubernetes", "err", err)
			return 1
		}
	} else {
		c.log.Error("error reading server certificate secret from kubernetes", "err", err)
		return 1
	}

	return 0
}

// getDaysAsDuration returns number of days the certificate
// is valid for as a time.Duration.
func (c *Command) getDaysAsDuration() time.Duration {
	duration, err := time.ParseDuration(fmt.Sprintf("%dh", 24*c.flagDays))
	if err != nil {
		c.log.Error("error parsing duration from days", "err", err)
		return 1
	}
	return duration
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.IntVar(&c.flagDays, "days", 1825, "The number of days the Consul server certificate is valid for from now on. Defaults to 5 years.")
	c.flags.StringVar(&c.flagDomain, "domain", "consul", "Domain of consul cluster. Only used in combination with -name-constraint. Defaults to consul.")
	c.flags.StringVar(&c.flagCaFile, "ca", "", "Path to the CA certificate file.")
	c.flags.StringVar(&c.flagKeyFile, "key", "", "Path to the CA key file.")
	c.flags.StringVar(&c.flagDC, "dc", "dc1", "Datacenter of the Consul cluster. Defaults to dc1.")
	c.flags.StringVar(&c.flagNamePrefix, "name-prefix", "", "Name prefix for secrets containing the CA, server certificate and private key")
	c.flags.StringVar(&c.flagK8sNamespace, "k8s-namespace", "default", "Name of Kubernetes namespace where secrets should be created and read from.")
	c.flags.Var(&c.flagDNSNames, "additional-dnsname", "Additional DNS name to add to the Consul server certificate as Subject Alternative Name. "+
		"localhost is always included. This flag may be provided multiple times.")
	c.flags.Var(&c.flagIPAddresses, "additional-ipaddress", "Additional IP address to add to the Consul server certificate as the Subject Alternative Name. "+
		"127.0.0.1 is always included. This flag may be provided multiple times.")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")
	c.k8sFlags = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8sFlags.Flags())
	c.help = flags.Usage(help, c.flags)
}

// configureKubeClient initialized the K8s clientset.
func (c *Command) configureKubeClient() error {
	config, err := subcommand.K8SConfig(c.k8sFlags.KubeConfig())
	if err != nil {
		return fmt.Errorf("error retrieving Kubernetes auth: %s", err)
	}
	c.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error initializing Kubernetes client: %s", err)
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

// caExists returns true if a CA certificate and key already
// exist and should be re-used.
func (c *Command) caExists() bool {
	if c.flagCaFile != "" && c.flagKeyFile != "" {
		return true
	}
	if c.caKeySecret != nil && c.caCertSecret != nil {
		return true
	}
	return false
}

// validateFlags returns an error if an invalid combination of
// flags are utilized.
func (c *Command) validateFlags() error {
	if (c.flagKeyFile != "" && c.flagCaFile == "") || (c.flagCaFile != "" && c.flagKeyFile == "") {
		return errors.New("either both -ca and -key or neither must be set")
	}
	if c.flagNamePrefix == "" {
		return errors.New("-name-prefix must be set")
	}
	if c.flagDays <= 0 {
		return errors.New("-days must be a positive integer")
	}

	return nil
}

const synopsis = "Initialize CA and Server Certificates during Consul install."
const help = `
Usage: consul-k8s-control-plane tls-init [options]

  Bootstraps the installation with a CA certificate, CA private key and TLS Certificates
  for the Consul server. It manages the rotation of the Server certificates on subsequent
  runs. It can be provided with the CA certificate and key files on disk or can manage it's own CA.

`
