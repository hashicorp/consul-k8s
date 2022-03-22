package aclinit

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/mitchellh/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flags             *flag.FlagSet
	k8s               *flags.K8SFlags
	flagSecretName    string
	flagInitType      string
	flagNamespace     string
	flagACLDir        string
	flagTokenSinkFile string

	k8sClient kubernetes.Interface

	once sync.Once
	help string

	ctx context.Context
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagSecretName, "secret-name", "",
		"Name of secret to watch for an ACL token")
	c.flags.StringVar(&c.flagInitType, "init-type", "",
		"ACL init type. The only supported value is 'client'. If set to 'client' will write Consul client ACL config to an acl-config.json file in -acl-dir")
	c.flags.StringVar(&c.flagNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where the servers are deployed")
	c.flags.StringVar(&c.flagACLDir, "acl-dir", "/consul/aclconfig",
		"Directory name of shared volume where client acl config file acl-config.json will be written if -init-type=client")
	c.flags.StringVar(&c.flagTokenSinkFile, "token-sink-file", "",
		"Optional filepath to write acl token")

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error("Should have no non-flag arguments.")
		return 1
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

	// Check if the client secret exists yet
	// If not, wait until it does
	var secret string
	for {
		var err error
		secret, err = c.getSecret(c.flagSecretName)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error getting Kubernetes secret: %s", err))
		}
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if c.flagInitType == "client" {
		// Construct extra client config json with acl details
		// This will be mounted as a volume for the client to use
		var buf bytes.Buffer
		tpl := template.Must(template.New("root").Parse(strings.TrimSpace(clientACLConfigTpl)))
		err := tpl.Execute(&buf, secret)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating template: %s", err))
			return 1
		}

		// Write the data out as a file.
		// Must be 0644 because this is written by the consul-k8s user but needs
		// to be readable by the consul user.
		err = ioutil.WriteFile(filepath.Join(c.flagACLDir, "acl-config.json"), buf.Bytes(), 0644)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error writing config file: %s", err))
			return 1
		}
	}

	if c.flagTokenSinkFile != "" {
		// Must be 0600 in case this command is re-run. In that case we need
		// to have permissions to overwrite our file.
		err := ioutil.WriteFile(c.flagTokenSinkFile, []byte(secret), 0600)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error writing token to file %q: %s", c.flagTokenSinkFile, err))
			return 1
		}
	}

	return 0
}

func (c *Command) getSecret(secretName string) (string, error) {
	secret, err := c.k8sClient.CoreV1().Secrets(c.flagNamespace).Get(c.ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// Extract token
	return string(secret.Data["token"]), nil
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
