package createfederationsecret

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	fedSecretGossipKey           = "gossipEncryptionKey"
	fedSecretCACertKey           = "caCert"
	fedSecretCAKeyKey            = "caKey"
	fedSecretServerConfigKey     = "serverConfigJSON"
	fedSecretReplicationTokenKey = "replicationToken"
)

var retryInterval = 1 * time.Second

type Command struct {
	UI    cli.Ui
	flags *flag.FlagSet
	k8s   *flags.K8SFlags
	http  *flags.HTTPFlags

	// flagExportReplicationToken controls whether we include the acl replication
	// token in the secret.
	flagExportReplicationToken bool
	flagGossipKeyFile          string

	// flagServerCACertFile is the location of the file containing the CA cert
	// for servers. We also accept a -ca-file flag. This will point to a different
	// file when auto-encrypt is enabled, otherwise it will point to the same file
	// as -server-ca-cert-file.
	// When auto-encrypt is enabled, the clients
	// use a different CA than the servers (since they piggy-back on the Connect CA)
	// and so when talking to our local client we need to use the CA cert passed
	// via -ca-file, not the server CA.
	flagServerCACertFile       string
	flagServerCAKeyFile        string
	flagResourcePrefix         string
	flagK8sNamespace           string
	flagLogLevel               string
	flagLogJSON                bool
	flagMeshGatewayServiceName string

	k8sClient    kubernetes.Interface
	consulClient *api.Client

	once sync.Once
	help string
	ctx  context.Context
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.BoolVar(&c.flagExportReplicationToken, "export-replication-token", false,
		"Set to true if the ACL replication token should be contained in the created secret. "+
			"If ACLs are enabled this should be set to true.")
	c.flags.StringVar(&c.flagGossipKeyFile, "gossip-key-file", "",
		"Location of a file containing the gossip encryption key. If not set, the created secret won't have a gossip encryption key.")
	c.flags.StringVar(&c.flagServerCACertFile, "server-ca-cert-file", "",
		"Location of a file containing the servers' CA certificate.")
	c.flags.StringVar(&c.flagServerCAKeyFile, "server-ca-key-file", "",
		"Location of a file containing the servers' CA signing key.")
	c.flags.StringVar(&c.flagResourcePrefix, "resource-prefix", "",
		"Prefix to use for Kubernetes resources. The created secret will be named '<resource-prefix>-federation'.")
	c.flags.StringVar(&c.flagK8sNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where Consul is deployed.")
	c.flags.StringVar(&c.flagMeshGatewayServiceName, "mesh-gateway-service-name", "",
		"Name of the mesh gateway service registered into Consul.")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.http = &flags.HTTPFlags{}
	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.http.Flags())
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

// Run creates a Kubernetes secret with data needed by secondary datacenters
// in order to federate with the primary. It's assumed this is running in the
// primary datacenter.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.validateFlags(args); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	logger, err := common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	// The initial secret struct. We will be filling in its data map
	// as we continue.
	federationSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-federation", c.flagResourcePrefix),
			Namespace: c.flagK8sNamespace,
			Labels:    map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Type: "Opaque",
		Data: make(map[string][]byte),
	}

	// Add gossip encryption key if it exists.
	if c.flagGossipKeyFile != "" {
		logger.Info("Retrieving gossip encryption key data")
		gossipKey, err := ioutil.ReadFile(c.flagGossipKeyFile)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error reading gossip encryption key file: %s", err))
			return 1
		}
		if len(gossipKey) == 0 {
			c.UI.Error(fmt.Sprintf("gossip key file %q was empty", c.flagGossipKeyFile))
			return 1
		}
		federationSecret.Data[fedSecretGossipKey] = gossipKey
		logger.Info("Gossip encryption key retrieved successfully")
	}

	// Add server CA cert.
	logger.Info("Retrieving server CA cert data")
	caCert, err := ioutil.ReadFile(c.flagServerCACertFile)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error reading server CA cert file: %s", err))
		return 1
	}
	federationSecret.Data[fedSecretCACertKey] = caCert
	logger.Info("Server CA cert retrieved successfully")

	// Add server CA key.
	logger.Info("Retrieving server CA key data")
	caKey, err := ioutil.ReadFile(c.flagServerCAKeyFile)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error reading server CA key file: %s", err))
		return 1
	}
	federationSecret.Data[fedSecretCAKeyKey] = caKey
	logger.Info("Server CA key retrieved successfully")

	// Create the Kubernetes clientset.
	if c.k8sClient == nil {
		k8sCfg, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}
		c.k8sClient, err = kubernetes.NewForConfig(k8sCfg)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	// Add replication token.
	var replicationToken []byte
	if c.flagExportReplicationToken {
		var err error
		replicationToken, err = c.replicationToken(logger)
		if err != nil {
			logger.Error("error retrieving replication token", "err", err)
			return 1
		}
		federationSecret.Data[fedSecretReplicationTokenKey] = replicationToken
	}

	// Set up Consul client because we need to make calls to Consul to retrieve
	// the datacenter name and mesh gateway addresses.
	if c.consulClient == nil {
		consulCfg := &api.Config{
			// Use the replication token for our ACL token. If ACLs are disabled,
			// this will be empty which won't matter because ACLs are disabled.
			Token: string(replicationToken),
		}
		// Merge our base config containing the optional ACL token with client
		// config automatically parsed from the passed flags and environment
		// variables. For example, when running in k8s the CONSUL_HTTP_ADDR environment
		// variable will be set to the IP of the Consul client pod on the same
		// node.
		c.http.MergeOntoConfig(consulCfg)

		var err error
		c.consulClient, err = consul.NewClient(consulCfg)
		if err != nil {
			logger.Error("Error creating consul client", "err", err)
			return 1
		}
	}

	// Get the datacenter's name. We assume this is the primary datacenter
	// because users should only be running this in the primary datacenter.
	logger.Info("Retrieving datacenter name from Consul")
	datacenter := c.consulDatacenter(logger)
	logger.Info("Successfully retrieved datacenter name")

	// Get the mesh gateway addresses.
	logger.Info("Retrieving mesh gateway addresses from Consul")
	meshGWAddrs, err := c.meshGatewayAddrs(logger)
	if err != nil {
		logger.Error("Error looking up mesh gateways", "err", err)
		return 1
	}
	logger.Info("Found mesh gateway addresses", "addrs", strings.Join(meshGWAddrs, ","))

	// Generate a JSON config from the datacenter and mesh gateway addresses
	// that can be set as a config file by Consul servers in secondary datacenters.
	serverCfg, err := c.serverCfg(datacenter, meshGWAddrs)
	if err != nil {
		logger.Error("Unable to create server config json", "err", err)
		return 1
	}
	federationSecret.Data[fedSecretServerConfigKey] = serverCfg

	// Now create the Kubernetes secret.
	logger.Info("Creating/updating Kubernetes secret", "name", federationSecret.ObjectMeta.Name, "ns", c.flagK8sNamespace)
	_, err = c.k8sClient.CoreV1().Secrets(c.flagK8sNamespace).Create(c.ctx, federationSecret, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		logger.Info("Secret already exists, updating instead")
		_, err = c.k8sClient.CoreV1().Secrets(c.flagK8sNamespace).Update(c.ctx, federationSecret, metav1.UpdateOptions{})
	}

	if err != nil {
		logger.Error("Error creating/updating federation secret", "err", err)
		return 1
	}
	logger.Info("Successfully created/updated federation secret", "name", federationSecret.ObjectMeta.Name, "ns", c.flagK8sNamespace)
	return 0
}

func (c *Command) validateFlags(args []string) error {
	if err := c.flags.Parse(args); err != nil {
		return err
	}
	if len(c.flags.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	if c.flagResourcePrefix == "" {
		return errors.New("-resource-prefix must be set")
	}
	if c.flagK8sNamespace == "" {
		return errors.New("-k8s-namespace must be set")
	}
	if c.flagServerCACertFile == "" {
		return errors.New("-server-ca-cert-file must be set")
	}
	if c.flagServerCAKeyFile == "" {
		return errors.New("-server-ca-key-file must be set")
	}
	if c.flagMeshGatewayServiceName == "" {
		return errors.New("-mesh-gateway-service-name must be set")
	}
	if err := c.validateCAFileFlag(); err != nil {
		return err
	}
	return nil
}

// replicationToken waits for the ACL replication token Kubernetes secret to
// be created and then returns it.
func (c *Command) replicationToken(logger hclog.Logger) ([]byte, error) {
	secretName := fmt.Sprintf("%s-%s-acl-token", c.flagResourcePrefix, common.ACLReplicationTokenName)
	logger.Info("Retrieving replication token from secret", "secret", secretName, "ns", c.flagK8sNamespace)

	var unrecoverableErr error
	var token []byte

	// Run in a retry loop because the replication secret will only exist once
	// ACL bootstrapping is complete. This can take some time because it
	// requires all servers to be running and a leader elected.
	// This will run forever but it's running as a Helm hook so Helm will timeout
	// after a configurable time period.
	err := backoff.Retry(func() error {
		secret, err := c.k8sClient.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, secretName, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			logger.Warn("secret not yet created, retrying", "secret", secretName, "ns", c.flagK8sNamespace)
			return errors.New("")
		} else if err != nil {
			unrecoverableErr = err
			return nil
		}
		var ok bool
		token, ok = secret.Data[common.ACLTokenSecretKey]
		if !ok {
			// If the secret exists but it doesn't have the expected key then
			// something must have gone wrong generating the secret and we
			// can't recover from that.
			unrecoverableErr = fmt.Errorf("expected key '%s' in secret %s not set", common.ACLTokenSecretKey, secretName)
			return nil
		}
		return nil
	}, backoff.NewConstantBackOff(retryInterval))
	// Unable to find the secret before timing out.
	if err != nil {
		return nil, err
	}

	if unrecoverableErr != nil {
		return nil, unrecoverableErr
	}
	logger.Info("Replication token retrieved successfully")
	return token, nil
}

// meshGatewayAddrs returns a list of unique WAN addresses for all service
// instances of the mesh-gateway service.
func (c *Command) meshGatewayAddrs(logger hclog.Logger) ([]string, error) {
	var meshGWSvcs []*api.CatalogService

	// Run in a retry in case the mesh gateways haven't yet been registered.
	_ = backoff.Retry(func() error {
		var err error
		meshGWSvcs, _, err = c.consulClient.Catalog().Service(c.flagMeshGatewayServiceName, "", nil)
		if err != nil {
			logger.Error("Error looking up mesh gateways, retrying", "err", err)
			return errors.New("")
		}
		if len(meshGWSvcs) < 1 {
			logger.Error("No instances of mesh gateway service found, retrying", "service-name", c.flagMeshGatewayServiceName)
			return errors.New("")
		}
		return nil
	}, backoff.NewConstantBackOff(retryInterval))

	// Use a map to collect the addresses to ensure uniqueness.
	meshGatewayAddrs := make(map[string]bool)
	for _, svc := range meshGWSvcs {
		addr, ok := svc.ServiceTaggedAddresses["wan"]
		if !ok {
			return nil, fmt.Errorf("no 'wan' key found in tagged addresses for service instance %q", svc.ServiceID)
		}
		meshGatewayAddrs[fmt.Sprintf("%s:%d", addr.Address, addr.Port)] = true
	}
	var uniqMeshGatewayAddrs []string
	for addr := range meshGatewayAddrs {
		uniqMeshGatewayAddrs = append(uniqMeshGatewayAddrs, addr)
	}
	return uniqMeshGatewayAddrs, nil
}

// serverCfg returns a JSON consul server config.
func (c *Command) serverCfg(datacenter string, gatewayAddrs []string) ([]byte, error) {
	type serverConfig struct {
		PrimaryDatacenter string   `json:"primary_datacenter"`
		PrimaryGateways   []string `json:"primary_gateways"`
	}
	return json.Marshal(serverConfig{
		PrimaryDatacenter: datacenter,
		PrimaryGateways:   gatewayAddrs,
	})
}

// consulDatacenter returns the current datacenter.
func (c *Command) consulDatacenter(logger hclog.Logger) string {
	// withLog is a helper method we'll use in the retry loop below to ensure
	// that errors are logged.
	var withLog = func(fn func() error) func() error {
		return func() error {
			err := fn()
			if err != nil {
				logger.Error("Error retrieving current datacenter, retrying", "err", err)
			}
			return err
		}
	}

	// Run in a retry because the Consul clients may not be running yet.
	var dc string
	_ = backoff.Retry(withLog(func() error {
		agentCfg, err := c.consulClient.Agent().Self()
		if err != nil {
			return err
		}
		if _, ok := agentCfg["Config"]; !ok {
			return fmt.Errorf("/agent/self response did not contain Config key: %s", agentCfg)
		}
		if _, ok := agentCfg["Config"]["Datacenter"]; !ok {
			return fmt.Errorf("/agent/self response did not contain Config.Datacenter key: %s", agentCfg)
		}
		var ok bool
		dc, ok = agentCfg["Config"]["Datacenter"].(string)
		if !ok {
			return fmt.Errorf("could not cast Config.Datacenter as string: %s", agentCfg)
		}
		if dc == "" {
			return fmt.Errorf("value of Config.Datacenter was empty string: %s", agentCfg)
		}
		return nil
	}), backoff.NewConstantBackOff(retryInterval))

	return dc
}

// validateCAFileFlag returns an error if the -ca-file flag (or its env var
// CONSUL_CACERT) isn't set or the file it points to can't be read.
func (c *Command) validateCAFileFlag() error {
	cfg := api.DefaultConfig()
	c.http.MergeOntoConfig(cfg)
	if cfg.TLSConfig.CAFile == "" {
		return errors.New("-ca-file or CONSUL_CACERT must be set")
	}
	_, err := ioutil.ReadFile(cfg.TLSConfig.CAFile)
	if err != nil {
		return fmt.Errorf("error reading CA file: %s", err)
	}
	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Create a Kubernetes secret containing data needed for federation"
const help = `
Usage: consul-k8s-control-plane create-federation-secret [options]

  Creates a Kubernetes secret that contains all the data required for a secondary
  datacenter to federate with the primary. This command should only be run in the
  primary datacenter.

`
