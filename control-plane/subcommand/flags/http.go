package flags

import (
	"flag"
	"io/ioutil"
	"strings"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/api"
)

// Taken from https://github.com/hashicorp/consul/blob/b5b9c8d953cd3c79c6b795946839f4cf5012f507/command/flags/http.go
// with flags we don't use removed. This was done so we don't depend on internal
// Consul implementation.

// HTTPFlags are flags used to configure communication with a Consul agent.
type HTTPFlags struct {
	address          StringValue
	token            StringValue
	tokenFile        StringValue
	caFile           StringValue
	caPath           StringValue
	certFile         StringValue
	keyFile          StringValue
	tlsServerName    StringValue
	partition        StringValue
	consulAPITimeout DurationValue
}

func (f *HTTPFlags) Flags() *flag.FlagSet {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.Var(&f.address, "http-addr",
		"The `address` and port of the Consul HTTP agent. The value can be an IP "+
			"address or DNS address, but it must also include the port. This can "+
			"also be specified via the CONSUL_HTTP_ADDR environment variable. The "+
			"default value is http://127.0.0.1:8500. The scheme can also be set to "+
			"HTTPS by setting the environment variable CONSUL_HTTP_SSL=true.")
	fs.Var(&f.token, "token",
		"ACL token to use in the request. This can also be specified via the "+
			"CONSUL_HTTP_TOKEN environment variable. If unspecified, the query will "+
			"default to the token of the Consul agent at the HTTP address.")
	fs.Var(&f.tokenFile, "token-file",
		"File containing the ACL token to use in the request instead of one specified "+
			"via the -token argument or CONSUL_HTTP_TOKEN environment variable. "+
			"This can also be specified via the CONSUL_HTTP_TOKEN_FILE environment variable.")
	fs.Var(&f.caFile, "ca-file",
		"Path to a CA file to use for TLS when communicating with Consul. This "+
			"can also be specified via the CONSUL_CACERT environment variable.")
	fs.Var(&f.caPath, "ca-path",
		"Path to a directory of CA certificates to use for TLS when communicating "+
			"with Consul. This can also be specified via the CONSUL_CAPATH environment variable.")
	fs.Var(&f.certFile, "client-cert",
		"Path to a client cert file to use for TLS when 'verify_incoming' is enabled. This "+
			"can also be specified via the CONSUL_CLIENT_CERT environment variable.")
	fs.Var(&f.keyFile, "client-key",
		"Path to a client key file to use for TLS when 'verify_incoming' is enabled. This "+
			"can also be specified via the CONSUL_CLIENT_KEY environment variable.")
	fs.Var(&f.tlsServerName, "tls-server-name",
		"The server name to use as the SNI host when connecting via TLS. This "+
			"can also be specified via the CONSUL_TLS_SERVER_NAME environment variable.")
	fs.Var(&f.partition, "partition",
		"[Enterprise Only] Name of the Consul Admin Partition to query. Default to \"default\" if Admin Partitions are enabled.")
	fs.Var(&f.consulAPITimeout, "consul-api-timeout",
		"The time in seconds that the consul API client will wait for a response from the API before cancelling the request.")
	return fs
}

func (f *HTTPFlags) Addr() string {
	return f.address.String()
}

func (f *HTTPFlags) ConsulAPITimeout() time.Duration {
	return f.consulAPITimeout.Duration()
}

func (f *HTTPFlags) Token() string {
	return f.token.String()
}

func (f *HTTPFlags) SetToken(v string) error {
	return f.token.Set(v)
}

func (f *HTTPFlags) TokenFile() string {
	return f.tokenFile.String()
}

func (f *HTTPFlags) SetTokenFile(v string) error {
	return f.tokenFile.Set(v)
}

func (f *HTTPFlags) TLSServerName() string {
	return f.tlsServerName.String()
}

func (f *HTTPFlags) ReadTokenFile() (string, error) {
	tokenFile := f.tokenFile.String()
	if tokenFile == "" {
		return "", nil
	}

	data, err := ioutil.ReadFile(tokenFile)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func (f *HTTPFlags) Partition() string {
	return f.partition.String()
}

func (f *HTTPFlags) APIClient() (*api.Client, error) {
	c := api.DefaultConfig()

	f.MergeOntoConfig(c)

	return consul.NewClient(c, f.ConsulAPITimeout())
}

func (f *HTTPFlags) MergeOntoConfig(c *api.Config) {
	f.address.Merge(&c.Address)
	f.token.Merge(&c.Token)
	f.tokenFile.Merge(&c.TokenFile)
	f.caFile.Merge(&c.TLSConfig.CAFile)
	f.caPath.Merge(&c.TLSConfig.CAPath)
	f.certFile.Merge(&c.TLSConfig.CertFile)
	f.keyFile.Merge(&c.TLSConfig.KeyFile)
	f.tlsServerName.Merge(&c.TLSConfig.Address)
	f.partition.Merge(&c.Partition)
}

func Merge(dst, src *flag.FlagSet) {
	if dst == nil {
		panic("dst cannot be nil")
	}
	if src == nil {
		return
	}
	src.VisitAll(func(f *flag.Flag) {
		dst.Var(f.Value, f.Name, f.Usage)
	})
}

// StringValue provides a flag value that's aware if it has been set.
type StringValue struct {
	v *string
}

// Merge will overlay this value if it has been set.
func (s *StringValue) Merge(onto *string) {
	if s.v != nil {
		*onto = *(s.v)
	}
}

// Set implements the flag.Value interface.
func (s *StringValue) Set(v string) error {
	if s.v == nil {
		s.v = new(string)
	}
	*(s.v) = v
	return nil
}

// String implements the flag.Value interface.
func (s *StringValue) String() string {
	var current string
	if s.v != nil {
		current = *(s.v)
	}
	return current
}

// DurationValue provides a flag value that's aware if it has been set.
type DurationValue struct {
	v *time.Duration
}

// Merge will overlay this value if it has been set.
func (d *DurationValue) Merge(onto *time.Duration) {
	if d.v != nil {
		*onto = *(d.v)
	}
}

// Set implements the flag.Value interface.
func (d *DurationValue) Set(v string) error {
	if d.v == nil {
		d.v = new(time.Duration)
	}
	var err error
	*(d.v), err = time.ParseDuration(v)
	return err
}

// String implements the flag.Value interface.
func (d *DurationValue) String() string {
	var current time.Duration
	if d.v != nil {
		current = *(d.v)
	}
	return current.String()
}

// String implements the flag.Value interface.
func (d *DurationValue) Duration() time.Duration {
	var current time.Duration
	if d.v != nil {
		current = *(d.v)
	}
	return current
}
