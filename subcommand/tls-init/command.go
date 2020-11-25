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

	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul-k8s/subcommand"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	ca string
	pk string
)

type Command struct {
	UI        cli.Ui
	clientset kubernetes.Interface

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	// flags that support the CA/key as files on disk.
	caFile  string
	keyFile string

	// flags that support the CA/key as secrets in Kubernetes.
	caCertSecret *corev1.Secret
	caKeySecret  *corev1.Secret

	// flags that dictate the specifications of the created certs.
	days        int
	domain      string
	dc          string
	dnsnames    flags.AppendSliceValue
	ipaddresses flags.AppendSliceValue

	// flags that dictate specifics for the secret name and namespace
	// that are created by the command.
	k8sNamespace string
	namePrefix   string

	// log
	log          hclog.Logger
	flagLogLevel string

	ctx context.Context

	once sync.Once
	help string
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
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
	c.log, err = common.Logger(c.flagLogLevel)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.clientset == nil {
		if err := c.configureKubeClient(); err != nil {
			c.log.Error("error configuring kubernetes client", "err", err.Error())
			return 1
		}
	}

	var cancel context.CancelFunc
	c.ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Verify if CA cert and key exist as Kubernetes secrets if they are not provided as file.
	if c.caFile == "" && c.keyFile == "" {
		c.caCertSecret, err = c.clientset.CoreV1().Secrets(c.k8sNamespace).Get(c.ctx, fmt.Sprintf("%s-ca-cert", c.namePrefix), metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			c.log.Error("error reading secret from kubernetes client", "err", err)
			return 1
		} else if err != nil {
			c.caCertSecret = nil
		}
		c.caKeySecret, err = c.clientset.CoreV1().Secrets(c.k8sNamespace).Get(c.ctx, fmt.Sprintf("%s-ca-key", c.namePrefix), metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			c.log.Error("error reading secret from kubernetes client", "err", err)
			return 1
		} else if err != nil {
			c.caKeySecret = nil
		}
	}

	// Only create a CA certificate/key pair if it doesn't exist or hasn't been provided
	if !c.existingCA() {
		c.log.Info("creating CA certificate and key as it does not exists")
		_, pk, ca, _, err = cert.GenerateCA("Consul Agent CA")
		if err != nil {
			c.log.Error("error generating Consul Agent CA certificate and private key", "err", err)
			return 1
		}

		c.log.Info("saving ca certificate")
		_, err = c.clientset.CoreV1().Secrets(c.k8sNamespace).Create(c.ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-ca-cert", c.namePrefix),
				Namespace: c.k8sNamespace,
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
		c.log.Info("saving ca private key")
		_, err = c.clientset.CoreV1().Secrets(c.k8sNamespace).Create(c.ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-ca-key", c.namePrefix),
				Namespace: c.k8sNamespace,
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
		c.log.Info("successfully saved ca and private key")
	}

	var hosts []string
	var name string
	var caBytes, keyBytes []byte

	if c.caFile != "" && c.keyFile != "" {
		c.log.Info("reading CA certificate from provided file")
		caBytes, err = ioutil.ReadFile(c.caFile)
		if err != nil {
			c.log.Error("error reading provided CA file", "err", err)
			return 1
		}
		c.log.Info("reading CA private key from provided file")
		keyBytes, err = ioutil.ReadFile(c.keyFile)
		if err != nil {
			c.log.Error("error reading provided private key file", "err", err)
			return 1
		}
	} else {
		if c.caCertSecret == nil {
			c.log.Info("reading CA certificate from secret in cluster")
			c.caCertSecret, err = c.clientset.CoreV1().Secrets(c.k8sNamespace).Get(c.ctx, fmt.Sprintf("%s-ca-cert", c.namePrefix), metav1.GetOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				c.log.Error("error reading secret from kubernetes client", "err", err)
				return 1
			}
		}
		if c.caKeySecret == nil {
			c.log.Info("reading CA private key from secret in cluster")
			c.caKeySecret, err = c.clientset.CoreV1().Secrets(c.k8sNamespace).Get(c.ctx, fmt.Sprintf("%s-ca-key", c.namePrefix), metav1.GetOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				c.log.Error("error reading secret from kubernetes client", "err", err)
				return 1
			}
		}
		caBytes = c.caCertSecret.Data[corev1.TLSCertKey]
		keyBytes = c.caKeySecret.Data[corev1.TLSPrivateKeyKey]
	}
	ca = string(caBytes)
	pk = string(keyBytes)

	for _, d := range c.dnsnames {
		if len(d) > 0 {
			hosts = append(hosts, strings.TrimSpace(d))
		}
	}

	for _, i := range c.ipaddresses {
		if len(i) > 0 {
			hosts = append(hosts, strings.TrimSpace(i))
		}
	}

	name = fmt.Sprintf("server.%s.%s", c.dc, c.domain)
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

	serverCertSecret, err := c.clientset.CoreV1().Secrets(c.k8sNamespace).Get(c.ctx, fmt.Sprintf("%s-server-cert", c.namePrefix), metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		c.log.Info("creating server certificate and private key secret")
		_, err := c.clientset.CoreV1().Secrets(c.k8sNamespace).Create(c.ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: c.k8sNamespace,
				Name:      fmt.Sprintf("%s-server-cert", c.namePrefix),
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
		_, err := c.clientset.CoreV1().Secrets(c.k8sNamespace).Update(c.ctx, serverCertSecret, metav1.UpdateOptions{})
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
	duration, err := time.ParseDuration(fmt.Sprintf("%dh", 24*c.days))
	if err != nil {
		c.log.Error("error parsing duration from days", "err", err)
		return 1
	}
	return duration
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.IntVar(&c.days, "days", 1825, "Provide number of days the CA is valid for from now on. Defaults to 5 years.")
	c.flags.StringVar(&c.domain, "domain", "consul", "Domain of consul cluster. Only used in combination with -name-constraint. Defaults to consul.")
	c.flags.StringVar(&c.caFile, "ca", "", "Provide path to the ca.")
	c.flags.StringVar(&c.keyFile, "key", "", "Provide path to the key.")
	c.flags.StringVar(&c.dc, "dc", "dc1", "Provide the datacenter. Matters only for -server certificates. Defaults to dc1.")
	c.flags.StringVar(&c.namePrefix, "name-prefix", "", "Provide the name prefix for secrets created with the ca, certificate and private key")
	c.flags.StringVar(&c.k8sNamespace, "k8s-namespace", "default", "Name of Kubernetes namespace where Consul and consul-k8s components are deployed..")
	c.flags.Var(&c.dnsnames, "additional-dnsname", "Provide an additional dnsname for Subject Alternative Names. "+
		"localhost is always included. This flag may be provided multiple times.")
	c.flags.Var(&c.ipaddresses, "additional-ipaddress", "Provide an additional ipaddress for Subject Alternative Names. "+
		"127.0.0.1 is always included. This flag may be provided multiple times.")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

// configureKubeClient initialized the K8s clientset.
func (c *Command) configureKubeClient() error {
	config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
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

// existingCA returns true if a CA certificate and key already
// exist and should be re-used.
func (c *Command) existingCA() bool {
	if c.caFile != "" && c.keyFile != "" {
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
	if (c.keyFile != "" && c.caFile == "") || (c.caFile != "" && c.keyFile == "") {
		return errors.New("either both -ca and -key or neither must be set")
	}
	if c.namePrefix == "" {
		return errors.New("-name-prefix must be set")
	}
	if c.days < 0 {
		return errors.New("-days must be a positive integer")
	}

	return nil
}

const synopsis = "TBD"
const help = `
Usage: consul-k8s tls-init [options]

  TBD

`
