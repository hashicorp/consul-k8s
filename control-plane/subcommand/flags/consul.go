// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package flags

import (
	"crypto/tls"
	"flag"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-rootcerts"
)

const (
	AddressesEnvVar = "CONSUL_ADDRESSES"
	GRPCPortEnvVar  = "CONSUL_GRPC_PORT"
	HTTPPortEnvVar  = "CONSUL_HTTP_PORT"

	NamespaceEnvVar  = "CONSUL_NAMESPACE"
	PartitionEnvVar  = "CONSUL_PARTITION"
	DatacenterEnvVar = "CONSUL_DATACENTER"

	ACLTokenEnvVar     = "CONSUL_ACL_TOKEN"
	ACLTokenFileEnvVar = "CONSUL_ACL_TOKEN_FILE"

	LoginAuthMethodEnvVar      = "CONSUL_LOGIN_AUTH_METHOD"
	LoginBearerTokenFileEnvVar = "CONSUL_LOGIN_BEARER_TOKEN_FILE"
	LoginDatacenterEnvVar      = "CONSUL_LOGIN_DATACENTER"
	LoginPartitionEnvVar       = "CONSUL_LOGIN_PARTITION"
	LoginNamespaceEnvVar       = "CONSUL_LOGIN_NAMESPACE"
	LoginMetaEnvVar            = "CONSUL_LOGIN_META"

	SkipServerWatchEnvVar = "CONSUL_SKIP_SERVER_WATCH"

	APITimeoutEnvVar = "CONSUL_API_TIMEOUT"
)

// ConsulFlags is a set of flags used to connect to Consul (servers).
type ConsulFlags struct {
	Addresses  string
	GRPCPort   int
	HTTPPort   int
	APITimeout time.Duration

	Namespace  string
	Partition  string
	Datacenter string

	SkipServerWatch bool

	ConsulTLSFlags
	ConsulACLFlags
}

type ConsulTLSFlags struct {
	UseTLS        bool
	CACertFile    string
	CACertPEM     string
	TLSServerName string
}

type ConsulACLFlags struct {
	ConsulLogin ConsulLoginFlags

	Token     string
	TokenFile string
}

type ConsulLoginFlags struct {
	AuthMethod      string
	BearerTokenFile string
	Datacenter      string
	Namespace       string
	Partition       string
	Meta            map[string]string
}

func (f *ConsulFlags) Flags() *flag.FlagSet {
	fs := flag.NewFlagSet("consul", flag.ContinueOnError)

	// Ignore parsing errors below because if we can't parse env variable because we want to
	// behave as if that env variable is not provided.
	grpcPort, _ := strconv.Atoi(os.Getenv(GRPCPortEnvVar))
	httpPort, _ := strconv.Atoi(os.Getenv(HTTPPortEnvVar))
	useTLS, _ := strconv.ParseBool(os.Getenv(constants.UseTLSEnvVar))
	skipServerWatch, _ := strconv.ParseBool(os.Getenv(SkipServerWatchEnvVar))
	consulLoginMetaFromEnv := os.Getenv(LoginMetaEnvVar)
	if consulLoginMetaFromEnv != "" {
		// Parse meta from env var.
		metaKeyValuePairs := strings.Split(consulLoginMetaFromEnv, ",")
		for _, metaKeyValue := range metaKeyValuePairs {
			kvList := strings.Split(metaKeyValue, "=")
			// We want to skip setting meta from env var if the key-value pairs are not provided correctly.
			if len(kvList) == 2 {
				if f.ConsulLogin.Meta == nil {
					f.ConsulLogin.Meta = make(map[string]string)
				}
				f.ConsulLogin.Meta[kvList[0]] = kvList[1]
			}
		}
	}

	defaultConsulLoginBearerTokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	if bearerTokenFileEnvVar := os.Getenv(LoginBearerTokenFileEnvVar); bearerTokenFileEnvVar != "" {
		defaultConsulLoginBearerTokenFile = bearerTokenFileEnvVar
	}

	defaultAPITimeout := 5 * time.Second
	if apiTimeoutEnv := os.Getenv(APITimeoutEnvVar); apiTimeoutEnv != "" {
		parsedAPITimeout, _ := time.ParseDuration(apiTimeoutEnv)
		if parsedAPITimeout != 0 {
			defaultAPITimeout = parsedAPITimeout
		}
	}

	fs.StringVar(&f.Addresses, "addresses", os.Getenv(AddressesEnvVar),
		"Consul server addresses. Can also be provided via CONSUL_ADDRESSES environment variable. "+
			"Value can be:\n"+
			"1. DNS name (that resolves to servers or DNS name of a load-balancer front of Consul servers) or an IP address; OR\n"+
			"2.'exec=<executable with optional args>'. The executable\n"+
			"	a) on success - should exit 0 and print to stdout whitespace delimited IP (v4/v6) addresses\n"+
			"	b) on failure - exit with a non-zero code and optionally print an error message of upto 1024 bytes to stderr.\n"+
			"	Refer to https://github.com/hashicorp/go-netaddrs#summary for more details and examples.")
	fs.IntVar(&f.GRPCPort, "grpc-port", grpcPort,
		"gRPC port to use when connecting to Consul servers.")
	fs.IntVar(&f.HTTPPort, "http-port", httpPort,
		"HTTP or HTTPs port to use when connecting to Consul servers.")
	fs.StringVar(&f.Namespace, "namespace", os.Getenv(NamespaceEnvVar),
		"[Enterprise only] Consul namespace.")
	fs.StringVar(&f.Partition, "partition", os.Getenv(PartitionEnvVar),
		"[Enterprise only] Consul admin partition. Default to \"default\" if Admin Partitions are enabled.")
	fs.StringVar(&f.Datacenter, "datacenter", os.Getenv(DatacenterEnvVar),
		"Consul datacenter.")
	fs.StringVar(&f.CACertFile, "ca-cert-file", os.Getenv(constants.CACertFileEnvVar),
		"Path to a CA certificate to use for TLS when communicating with Consul.")
	fs.StringVar(&f.CACertPEM, "ca-cert-pem", os.Getenv(constants.CACertPEMEnvVar),
		"CA certificate PEM to use for TLS when communicating with Consul.")
	fs.StringVar(&f.TLSServerName, "tls-server-name", os.Getenv(constants.TLSServerNameEnvVar),
		"The server name to use as the SNI host when connecting via TLS. "+
			"This can also be specified via the CONSUL_TLS_SERVER_NAME environment variable.")
	fs.BoolVar(&f.UseTLS, "use-tls", useTLS, "If true, use TLS for connections to Consul.")
	fs.StringVar(&f.Token, "token", os.Getenv(ACLTokenEnvVar),
		"ACL token to use for connection to Consul."+
			"This can also be specified via the CONSUL_ACL_TOKEN environment variable.")
	fs.StringVar(&f.TokenFile, "token-file", os.Getenv(ACLTokenFileEnvVar),
		"ACL token file to use for connection to Consul."+
			"This can also be specified via the CONSUL_ACL_TOKEN_FILE environment variable.")
	fs.StringVar(&f.ConsulLogin.AuthMethod, "auth-method-name", os.Getenv(LoginAuthMethodEnvVar),
		"Auth method name to use for login to Consul."+
			"This can also be specified via the CONSUL_LOGIN_AUTH_METHOD environment variable.")
	fs.StringVar(&f.ConsulLogin.BearerTokenFile, "consul-login-bearer-token-file", defaultConsulLoginBearerTokenFile,
		"Bearer token file to use for login to Consul."+
			"This can also be specified via the CONSUL_LOGIN_BEARER_TOKEN_FILE environment variable.")
	fs.StringVar(&f.ConsulLogin.Datacenter, "consul-login-datacenter", os.Getenv(LoginDatacenterEnvVar),
		"Auth method datacenter to use for login to Consul."+
			"This can also be specified via the CONSUL_LOGIN_DATACENTER environment variable.")
	fs.StringVar(&f.ConsulLogin.Partition, "consul-login-partition", os.Getenv(LoginPartitionEnvVar),
		"Auth method partition to use for login to Consul."+
			"This can also be specified via the CONSUL_LOGIN_PARTITION environment variable.")
	fs.StringVar(&f.ConsulLogin.Namespace, "consul-login-namespace", os.Getenv(LoginNamespaceEnvVar),
		"Auth method namespace to use for login to Consul."+
			"This can also be specified via the CONSUL_LOGIN_NAMESPACE environment variable.")
	fs.Var((*FlagMapValue)(&f.ConsulLogin.Meta), "consul-login-meta",
		"Metadata to set on the token, formatted as key=value. This flag "+
			"may be specified multiple times to set multiple meta fields.")
	fs.DurationVar(&f.APITimeout, "api-timeout", defaultAPITimeout,
		"The time in seconds that the consul API client will wait for a response from the API before cancelling the request.")
	fs.BoolVar(&f.SkipServerWatch, "skip-server-watch", skipServerWatch, "If true, skip watching server upstream."+
		"This can also be specified via the CONSUL_SKIP_SERVER_WATCH environment variable.")
	return fs
}

func (f *ConsulFlags) ConsulServerConnMgrConfig() (discovery.Config, error) {
	cfg := discovery.Config{
		Addresses: f.Addresses,
		GRPCPort:  f.GRPCPort,
	}

	if f.UseTLS {
		tlsConfig := &tls.Config{}
		if f.CACertFile != "" {
			err := rootcerts.ConfigureTLS(tlsConfig, &rootcerts.Config{
				CAFile: f.CACertFile,
			})
			if err != nil {
				return discovery.Config{}, err
			}
		} else if f.CACertPEM != "" {
			err := rootcerts.ConfigureTLS(tlsConfig, &rootcerts.Config{
				CACertificate: []byte(f.CACertPEM),
			})
			if err != nil {
				return discovery.Config{}, err
			}
		}
		tlsConfig.ServerName = f.TLSServerName
		cfg.TLS = tlsConfig
	}

	if f.ConsulLogin.AuthMethod != "" {
		cfg.Credentials.Type = discovery.CredentialsTypeLogin
		cfg.Credentials.Login.AuthMethod = f.ConsulLogin.AuthMethod
		cfg.Credentials.Login.Namespace = f.ConsulLogin.Namespace
		cfg.Credentials.Login.Partition = f.ConsulLogin.Partition
		cfg.Credentials.Login.Datacenter = f.ConsulLogin.Datacenter
		cfg.Credentials.Login.Meta = f.ConsulLogin.Meta

		bearerToken, err := os.ReadFile(f.ConsulLogin.BearerTokenFile)
		if err != nil {
			return discovery.Config{}, err
		}
		cfg.Credentials.Login.BearerToken = string(bearerToken)
	} else if f.Token != "" {
		cfg.Credentials.Type = discovery.CredentialsTypeStatic
		cfg.Credentials.Static.Token = f.Token
	} else if f.TokenFile != "" {
		token, err := os.ReadFile(f.TokenFile)
		if err != nil {
			return discovery.Config{}, err
		}
		cfg.Credentials.Type = discovery.CredentialsTypeStatic
		cfg.Credentials.Static.Token = string(token)
	}

	if f.SkipServerWatch {
		cfg.ServerWatchDisabled = true
	}

	return cfg, nil
}

func (f *ConsulFlags) ConsulClientConfig() *consul.Config {
	cfg := &api.Config{
		Namespace:  f.Namespace,
		Partition:  f.Partition,
		Datacenter: f.Datacenter,
		Scheme:     "http",
	}

	if f.UseTLS {
		cfg.Scheme = "https"
		if f.CACertFile != "" {
			cfg.TLSConfig.CAFile = f.CACertFile
		} else if f.CACertPEM != "" {
			cfg.TLSConfig.CAPem = []byte(f.CACertPEM)
		}

		// Infer TLS server name from addresses.
		if f.TLSServerName == "" && !strings.HasPrefix(f.Addresses, "exec=") {
			cfg.TLSConfig.Address = f.Addresses
		} else if f.TLSServerName != "" {
			cfg.TLSConfig.Address = f.TLSServerName
		}
	}

	if f.Token != "" {
		cfg.Token = f.Token
	} else if f.TokenFile != "" {
		cfg.TokenFile = f.TokenFile
	}

	return &consul.Config{
		APIClientConfig: cfg,
		HTTPPort:        f.HTTPPort,
		GRPCPort:        f.GRPCPort,
		APITimeout:      f.APITimeout,
	}
}
