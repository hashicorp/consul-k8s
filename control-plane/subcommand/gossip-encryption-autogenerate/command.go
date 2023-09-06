package gossipencryptionautogenerate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"sync"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	// These flags determine where the Kubernetes secret will be stored.
	flagNamespace  string
	flagSecretName string
	flagSecretKey  string

	flagLogLevel string
	flagLogJSON  bool

	k8sClient kubernetes.Interface

	log  hclog.Logger
	once sync.Once
	ctx  context.Context
	help string
}

// init is run once to set up usage documentation for flags.
func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false, "Enable or disable JSON output format for logging.")
	c.flags.StringVar(&c.flagNamespace, "namespace", "", "Name of Kubernetes namespace where Consul and consul-k8s components are deployed.")
	c.flags.StringVar(&c.flagSecretName, "secret-name", "", "Name of the secret to create.")
	c.flags.StringVar(&c.flagSecretKey, "secret-key", "key", "Name of the secret key to create.")

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())

	c.help = flags.Usage(help, c.flags)
}

// Run parses input and creates a gossip secret in Kubernetes if none exists at the given namespace and secret name.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.flags.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("Failed to parse args: %v", err))
		return 1
	}

	if err := c.validateFlags(); err != nil {
		c.UI.Error(fmt.Sprintf("Failed to validate flags: %v", err))
		return 1
	}

	var err error
	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	if c.k8sClient == nil {
		if err = c.createKubernetesClient(); err != nil {
			c.UI.Error(fmt.Sprintf("Failed to create Kubernetes client: %v", err))
			return 1
		}
	}

	if exists, err := c.doesKubernetesSecretExist(); err != nil {
		c.UI.Error(fmt.Sprintf("Failed to check if Kubernetes secret exists: %v", err))
		return 1
	} else if exists {
		// Safe exit if secret already exists.
		c.UI.Info(fmt.Sprintf("A Kubernetes secret with the name `%s` already exists.", c.flagSecretName))
		return 0
	}

	gossipSecret, err := generateGossipSecret()
	if err != nil {
		c.UI.Error(fmt.Sprintf("Failed to generate gossip secret: %v", err))
		return 1
	}

	// Create the Kubernetes secret object.
	kubernetesSecret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.flagSecretName,
			Namespace: c.flagNamespace,
			Labels:    map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Data: map[string][]byte{
			c.flagSecretKey: []byte(gossipSecret),
		},
	}

	// Write the secret to Kubernetes.
	_, err = c.k8sClient.CoreV1().Secrets(c.flagNamespace).Create(c.ctx, &kubernetesSecret, metav1.CreateOptions{})
	if err != nil {
		c.UI.Error(fmt.Sprintf("Failed to create Kubernetes secret: %v", err))
		return 1
	}

	c.UI.Info(fmt.Sprintf("Successfully created Kubernetes secret `%s` in namespace `%s`.", c.flagSecretName, c.flagNamespace))
	return 0
}

// Help returns the command's help text.
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

// Synopsis returns a one-line synopsis of the command.
func (c *Command) Synopsis() string {
	return synopsis
}

// validateFlags ensures that all required flags are set.
func (c *Command) validateFlags() error {
	if c.flagNamespace == "" {
		return fmt.Errorf("-namespace must be set")
	}

	if c.flagSecretName == "" {
		return fmt.Errorf("-secret-name must be set")
	}

	return nil
}

// createKubernetesClient creates a Kubernetes client on the command object.
func (c *Command) createKubernetesClient() error {
	config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes config: %v", err)
	}

	c.k8sClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error initializing Kubernetes client: %s", err)
	}

	return nil
}

// doesKubernetesSecretExist checks if a secret with the given name exists in the given namespace.
func (c *Command) doesKubernetesSecretExist() (bool, error) {
	_, err := c.k8sClient.CoreV1().Secrets(c.flagNamespace).Get(c.ctx, c.flagSecretName, metav1.GetOptions{})

	// If the secret does not exist, the error will be a NotFound error.
	if err != nil && apierrors.IsNotFound(err) {
		return false, nil
	}

	// If the error is not a NotFound error, return the error.
	if err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to get Kubernetes secret: %v", err)
	}

	// The secret exists.
	return true, nil
}

// generateGossipSecret generates a random 32 byte secret returned as a base64 encoded string.
func generateGossipSecret() (string, error) {
	// This code was copied from Consul's Keygen command:
	// https://github.com/hashicorp/consul/blob/d652cc86e3d0322102c2b5e9026c6a60f36c17a5/command/keygen/keygen.go

	key := make([]byte, 32)
	n, err := rand.Reader.Read(key)

	if err != nil {
		return "", fmt.Errorf("error reading random data: %s", err)
	}
	if n != 32 {
		return "", fmt.Errorf("couldn't read enough entropy")
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

const synopsis = "Generate and store a secret for gossip encryption."
const help = `
Usage: consul-k8s-control-plane gossip-encryption-autogenerate [options]

  Bootstraps the installation with a secret for gossip encryption.
`
