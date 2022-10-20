package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/cli"
	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

const ipv4RegEx = "(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)"

// TestInstall tests that we can install consul service mesh with the CLI
// and see that services can connect.
func TestInstall(t *testing.T) {
	secureCases := []bool{false, true}

	for _, secure := range secureCases {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cli, err := cli.NewCLI()
			require.NoError(t, err)

			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			connHelper := connhelper.ConnectHelper{
				ClusterKind: consul.CLI,
				Secure:      secure,
				ReleaseName: consul.CLIReleaseName,
				Ctx:         ctx,
				Cfg:         cfg,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			if secure {
				connHelper.TestConnectionFailureWithoutIntention(t)
				connHelper.CreateIntention(t)
			}

			// Run proxy list and get the two results.
			listOut, err := cli.Run(t, ctx.KubectlOptions(t), "proxy", "list")
			require.NoError(t, err)
			logger.Log(t, string(listOut))
			list := translateListOutput(listOut)
			require.Equal(t, 2, len(list))
			for _, proxyType := range list {
				require.Equal(t, "Sidecar", proxyType)
			}

			// Run proxy read and check that the connection is present in the output.
			retrier := &retry.Timer{Timeout: 160 * time.Second, Wait: 2 * time.Second}
			retry.RunWith(retrier, t, func(r *retry.R) {
				for podName := range list {
					out, err := cli.Run(t, ctx.KubectlOptions(t), "proxy", "read", podName)
					require.NoError(t, err)

					output := string(out)
					logger.Log(t, output)

					// Both proxies must see their own local agent and app as clusters.
					require.Regexp(r, "consul-dataplane.*STATIC", output)
					require.Regexp(r, "local_app.*STATIC", output)

					// Static Client must have Static Server as a cluster and endpoint.
					if strings.Contains(podName, "static-client") {
						require.Regexp(r, "static-server.*static-server\\.default\\.dc1\\.internal.*EDS", output)
						require.Regexp(r, ipv4RegEx+".*static-server", output)
					}
				}
			})

			connHelper.TestConnectionSuccess(t)
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}

// translateListOutput takes the raw output from the proxy list command and
// translates the table into a map.
func translateListOutput(raw []byte) map[string]string {
	formatted := make(map[string]string)
	for _, pod := range strings.Split(strings.TrimSpace(string(raw)), "\n")[3:] {
		row := strings.Split(strings.TrimSpace(pod), "\t")

		var name string
		if len(row) == 3 { // Handle the case where namespace is present
			name = fmt.Sprintf("%s/%s", strings.TrimSpace(row[0]), strings.TrimSpace(row[1]))
		} else if len(row) == 2 {
			name = strings.TrimSpace(row[0])
		}
		formatted[name] = row[len(row)-1]
	}

	return formatted
}
