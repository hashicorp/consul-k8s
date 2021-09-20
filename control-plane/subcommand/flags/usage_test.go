package flags

import (
	"flag"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsage(t *testing.T) {
	flags := flag.NewFlagSet("", flag.ContinueOnError)
	flags.String("flag1", "", "flag1 usage")
	http := &HTTPFlags{}
	Merge(flags, http.Flags())
	help := "Main help output"

	exp := `Main help output

HTTP API Options

  -ca-file=<value>
     Path to a CA file to use for TLS when communicating with Consul.
     This can also be specified via the CONSUL_CACERT environment
     variable.

  -ca-path=<value>
     Path to a directory of CA certificates to use for TLS when
     communicating with Consul. This can also be specified via the
     CONSUL_CAPATH environment variable.

  -client-cert=<value>
     Path to a client cert file to use for TLS when 'verify_incoming'
     is enabled. This can also be specified via the CONSUL_CLIENT_CERT
     environment variable.

  -client-key=<value>
     Path to a client key file to use for TLS when 'verify_incoming'
     is enabled. This can also be specified via the CONSUL_CLIENT_KEY
     environment variable.

  -http-addr=<address>
     The $address$ and port of the Consul HTTP agent. The value can be
     an IP address or DNS address, but it must also include the port.
     This can also be specified via the CONSUL_HTTP_ADDR environment
     variable. The default value is http://127.0.0.1:8500. The scheme
     can also be set to HTTPS by setting the environment variable
     CONSUL_HTTP_SSL=true.

  -partition=<value>
     [Enterprise Only] Name of the Consul Admin Partition to query.
     Default to "default" if Admin Partitions are enabled.

  -tls-server-name=<value>
     The server name to use as the SNI host when connecting via
     TLS. This can also be specified via the CONSUL_TLS_SERVER_NAME
     environment variable.

  -token=<value>
     ACL token to use in the request. This can also be specified via the
     CONSUL_HTTP_TOKEN environment variable. If unspecified, the query
     will default to the token of the Consul agent at the HTTP address.

  -token-file=<value>
     File containing the ACL token to use in the request instead of one
     specified via the -token argument or CONSUL_HTTP_TOKEN environment
     variable. This can also be specified via the CONSUL_HTTP_TOKEN_FILE
     environment variable.

Command Options

  -flag1=<string>
     flag1 usage`

	// Had to use $ instead of backticks above for multiline string.
	// Here we sub the backtick back in.
	exp = strings.Replace(exp, "$", "`", -1)
	require.Equal(t, exp, Usage(help, flags))
}
