package aclinit

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/hashicorp/consul-k8s/subcommand"
	k8sflags "github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/command/flags"
	"github.com/mitchellh/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flags             *flag.FlagSet
	k8s               *k8sflags.K8SFlags
	flagSecretName    string
	flagInitType      string
	flagACLDownPolicy string
	flagNamespace     string
	flagACLDir        string

	k8sClient *kubernetes.Clientset

	once sync.Once
	help string
}

type TemplatePayload struct {
	Secret        string
	ACLDownPolicy string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagSecretName, "secret-name", "",
		"Name of secret to watch for an ACL token")
	c.flags.StringVar(&c.flagInitType, "init-type", "",
		"ACL init target, valid values are `client` and `sync`")
	c.flags.StringVar(&c.flagACLDownPolicy, "acl-down-policy", "extend-cache",
		"ACL down-policy, valid values are `allow`, `deny`, `extend-cache` or `async-cache`")
	c.flags.StringVar(&c.flagNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where the servers are deployed")
	c.flags.StringVar(&c.flagACLDir, "acl-dir", "/consul/aclconfig",
		"Directory name of shared volume where acl config will be output")

	c.k8s = &k8sflags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error(fmt.Sprintf("Should have no non-flag arguments."))
		return 1
	}

	config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
		return 1
	}

	// Create the Kubernetes clientset
	c.k8sClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
		return 1
	}

	// Check if the client secret exists yet
	// If not, wait until it does
	var secret string
	for {
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
		payload := TemplatePayload{secret, c.flagACLDownPolicy}

		tpl := template.Must(template.New("root").Parse(strings.TrimSpace(clientACLConfigTpl)))
		err = tpl.Execute(&buf, payload)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating template: %s", err))
			return 1
		}

		// Write the data out as a file
		err = ioutil.WriteFile(filepath.Join(c.flagACLDir, "acl-config.json"), buf.Bytes(), 0644)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error writing config file: %s", err))
			return 1
		}
	}

	return 0
}

func (c *Command) getSecret(secretName string) (string, error) {
	secret, err := c.k8sClient.CoreV1().Secrets(c.flagNamespace).Get(secretName, metav1.GetOptions{})
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
Usage: consul-k8s acl-init [options]

  Bootstraps non-server components with ACLs by waiting for a
  secret to be populated with an ACL token to be used.

`

const clientACLConfigTpl = `
{
  "acl": {
    "enabled": true,
    "default_policy": "deny",
    "down_policy": "{{ .ACLDownPolicy }}",
    "tokens": {
      "agent": "{{ .Secret }}"
    }
  }
}
`
