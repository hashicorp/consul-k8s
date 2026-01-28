// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aclinit

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-netaddrs"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

const (
	defaultBearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultTokenSinkFile   = "/consul/login/acl-token"
)

type Command struct {
	UI cli.Ui

	flags  *flag.FlagSet
	k8s    *flags.K8SFlags
	consul *flags.ConsulFlags

	flagSecretName    string
	flagInitType      string
	flagACLDir        string
	flagTokenSinkFile string
	flagK8sNamespace  string

	flagLogLevel string
	flagLogJSON  bool

	k8sClient kubernetes.Interface

	once   sync.Once
	help   string
	logger hclog.Logger

	ctx          context.Context
	consulClient *api.Client
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagSecretName, "secret-name", "",
		"Name of secret to watch for an ACL token")
	c.flags.StringVar(&c.flagInitType, "init-type", "",
		"ACL init type. The only supported value is 'client'. If set to 'client' will write Consul client ACL config to an acl-config.json file in -acl-dir")
	c.flags.StringVar(&c.flagACLDir, "acl-dir", "/consul/aclconfig",
		"Directory name of shared volume where client acl config file acl-config.json will be written if -init-type=client")
	c.flags.StringVar(&c.flagTokenSinkFile, "token-sink-file", "",
		"Optional filepath to write acl token")

	// Flags related to using consul login to fetch the ACL token.
	c.flags.StringVar(&c.flagK8sNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where the token Kubernetes secret is stored.")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.k8s = &flags.K8SFlags{}
	c.consul = &flags.ConsulFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	flags.Merge(c.flags, c.consul.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)
	if err = c.flags.Parse(args); err != nil {
		return 1
	}
	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.consul.ConsulLogin.BearerTokenFile == "" {
		c.consul.ConsulLogin.BearerTokenFile = defaultBearerTokenFile
	}
	// This allows us to utilize the default path of `/consul/login/acl-token` for the ACL token
	// but only in the case of when we're using ACL.Login. If flagACLAuthMethod is not set and
	// the tokenSinkFile is also unset it means we do not want to write an ACL token in the case
	// of the client token.
	if c.flagTokenSinkFile == "" {
		c.flagTokenSinkFile = defaultTokenSinkFile
	}
	if c.flagK8sNamespace == "" {
		c.flagK8sNamespace = corev1.NamespaceDefault
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	// Create the Kubernetes clientset
	if c.k8sClient == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}
		c.k8sClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	// Set up logging.
	if c.logger == nil {
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	var secret string
	if c.consul.ConsulLogin.AuthMethod != "" {
		var ipAddrs []net.IPAddr
		if err := backoff.Retry(func() error {
			ipAddrs, err = netaddrs.IPAddrs(c.ctx, c.consul.Addresses, c.logger)
			if err != nil {
				c.logger.Error("Error resolving IP Address", "err", err)
				return err
			}
			return nil
		}, exponentialBackoffWithMaxInterval()); err != nil {
			c.UI.Error(err.Error())
			return 1
		}
		firstServerAddr := net.JoinHostPort(ipAddrs[0].IP.String(), strconv.Itoa(c.consul.HTTPPort))

		config := c.consul.ConsulClientConfig().APIClientConfig
		config.Address = firstServerAddr

		c.consulClient, err = consul.NewClient(config, c.consul.APITimeout)
		if err != nil {
			c.logger.Error("Failed to create Consul client", "error", err)
			return 1
		}

		loginParams := common.LoginParams{
			AuthMethod:      c.consul.ConsulLogin.AuthMethod,
			Datacenter:      c.consul.ConsulLogin.Datacenter,
			BearerTokenFile: c.consul.ConsulLogin.BearerTokenFile,
			TokenSinkFile:   c.flagTokenSinkFile,
			Meta:            c.consul.ConsulLogin.Meta,
		}
		secret, err = common.ConsulLogin(c.consulClient, loginParams, c.logger)
		if err != nil {
			c.logger.Error("Failed to login to Consul", "error", err)
			return 1
		}
		c.logger.Info("Successfully read ACL token from the server")
	} else {
		// Use k8s secret to obtain token.

		// Check if the client secret exists yet
		// If not, wait until it does.
		for {
			var err error
			secret, err = c.getSecret(c.flagSecretName)
			if err != nil {
				c.logger.Error("Error getting Kubernetes secret: ", "error", err)
			}
			if err == nil {
				c.logger.Info("Successfully read Kubernetes secret")
				break
			}
			time.Sleep(1 * time.Second)
		}
	}

	if c.flagInitType == "client" {
		// Construct extra client config json with acl details
		// This will be mounted as a volume for the client to use
		var buf bytes.Buffer
		tpl := template.Must(template.New("root").Parse(strings.TrimSpace(clientACLConfigTpl)))
		err := tpl.Execute(&buf, secret)
		if err != nil {
			c.logger.Error("Error creating template", "error", err)
			return 1
		}

		// Write the data out as a file.
		// Must be 0644 because this is written by the consul-k8s user but needs
		// to be readable by the consul user.
		err = os.WriteFile(filepath.Join(c.flagACLDir, "acl-config.json"), buf.Bytes(), 0644)
		if err != nil {
			c.logger.Error("Error writing config file", "error", err)
			return 1
		}
	}

	if c.flagTokenSinkFile != "" {
		// Must be 0600 in case this command is re-run. In that case we need
		// to have permissions to overwrite our file.
		err := os.WriteFile(c.flagTokenSinkFile, []byte(secret), 0600)
		if err != nil {
			c.logger.Error("Error writing token to file", "file", c.flagTokenSinkFile, "error", err)
			return 1
		}
	}

	return 0
}

func (c *Command) getSecret(secretName string) (string, error) {
	secret, err := c.k8sClient.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// Extract token
	return string(secret.Data["token"]), nil
}

func (c *Command) validateFlags() error {
	if len(c.flags.Args()) > 0 {
		return errors.New("Should have no non-flag arguments.")
	}
	if c.consul.APITimeout <= 0 {
		return errors.New("-consul-api-timeout must be set to a value greater than 0")
	}

	return nil
}

// exponentialBackoffWithMaxInterval creates an exponential backoff but limits the
// maximum backoff to 10 seconds so that we don't find ourselves in a situation
// where we are waiting for minutes before retries.
func exponentialBackoffWithMaxInterval() *backoff.ExponentialBackOff {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxInterval = 10 * time.Second
	backoff.Reset()
	return backoff
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Initialize ACLs on non-server components."
const help = `
Usage: consul-k8s-control-plane acl-init [options]

  Bootstraps non-server components with ACLs by waiting for a
  secret to be populated with an ACL token to be used.

`

const clientACLConfigTpl = `
{
  "acl": {
    "enabled": true,
    "default_policy": "deny",
    "down_policy": "extend-cache",
    "tokens": {
      "agent": "{{ . }}"
    }
  }
}
`
