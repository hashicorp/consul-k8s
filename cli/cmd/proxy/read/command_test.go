package read

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFlagParsing(t *testing.T) {
	cases := map[string]struct {
		args []string
		out  int
	}{
		"No args": {
			args: []string{},
			out:  1,
		},
		"Multiple podnames passed": {
			args: []string{"podname", "podname2"},
			out:  1,
		},
		"Nonexistent flag passed, -foo bar": {
			args: []string{"podName", "-foo", "bar"},
			out:  1,
		},
		"Invalid argument passed, -namespace YOLO": {
			args: []string{"podName", "-namespace", "YOLO"},
			out:  1,
		},
		"User passed incorrect output": {
			args: []string{"podName", "-output", "image"},
			out:  1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := getInitializedCommand(t)
			c.kubernetes = fake.NewSimpleClientset()
			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

func TestEndToEnd(t *testing.T) {
	// These regular expressions must be present in the output.
	expected := []string{
		"Envoy configuration for fakePodName in Namespace default:",

		"==> Clusters \\(6\\)",
		"Name.*FQDN.*Endpoints.*Type.*Last Updated",
		"local_agent.*local_agent.*192\\.168\\.79\\.187:8502.*STATIC.*2022-05-13T04:22:39\\.553Z",
		"local_app.*local_app.*127\\.0\\.0\\.1:8080.*STATIC.*2022-05-13T04:22:39\\.655Z",
		"client.*client\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul.*EDS.*2022-06-09T00:39:12\\.948Z",
		"frontend.*frontend\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul.*EDS.*2022-06-09T00:39:12\\.855Z",
		"original-destination.*original-destination.*ORIGINAL_DST.*2022-05-13T04:22:39.743Z",
		"server.*server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul.*EDS.*2022-06-09T00:39:12\\.754Z",

		"==> Endpoints \\(9\\)",
		"Address:Port.*Cluster.*Weight.*Status",
		"192.168.79.187:8502.*local_agent.*1.00.*HEALTHY",
		"127.0.0.1:8080.*local_app.*1.00.*HEALTHY",
		"192.168.31.201:20000.*1.00.*HEALTHY",
		"192.168.47.235:20000.*1.00.*HEALTHY",
		"192.168.71.254:20000.*1.00.*HEALTHY",
		"192.168.63.120:20000.*1.00.*HEALTHY",
		"192.168.18.110:20000.*1.00.*HEALTHY",
		"192.168.52.101:20000.*1.00.*HEALTHY",
		"192.168.65.131:20000.*1.00.*HEALTHY",

		"==> Listeners \\(2\\)",
		"Name.*Address:Port.*Direction.*Filter Chain Match.*Filters.*Last Updated",
		"public_listener.*192\\.168\\.69\\.179:20000.*INBOUND.*Any.*\\* -> local_app/.*2022-06-09T00:39:27\\.668Z",
		"outbound_listener.*127.0.0.1:15001.*OUTBOUND.*10\\.100\\.134\\.173/32, 240\\.0\\.0\\.3/32.*-> client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul.*2022-05-24T17:41:59\\.079Z",
		"10\\.100\\.254\\.176/32, 240\\.0\\.0\\.4/32.*\\* -> server\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul/",
		"10\\.100\\.31\\.2/32, 240\\.0\\.0\\.2/32.*-> frontend\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul",
		"Any.*-> original-destination",

		"==> Routes \\(2\\)",
		"Name.*Destination Cluster.*Last Updated",
		"public_listener.*local_app/.*2022-06-09T00:39:27.667Z",
		"server.*server\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul/.*2022-05-24T17:41:59\\.078Z",

		"==> Secrets \\(2\\)",
		"Name.*Type.*Last Updated",
		"default.*Dynamic Active.*2022-05-24T17:41:59.078Z",
		"ROOTCA.*Dynamic Warming.*2022-03-15T05:14:22.868Z",
	}

	// A fetchConfig function that just returns the test Envoy config.
	mockFetchConfig := func(context.Context, common.PortForwarder) (*EnvoyConfig, error) {
		return testEnvoyConfig, nil
	}

	c, buf := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	c.fetchConfig = mockFetchConfig

	out := c.Run([]string{"fakePodName"})
	require.Equal(t, 0, out)

	actual := buf.String()

	for _, expression := range expected {
		require.Regexp(t, expression, actual)
	}
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T) (*ReadCommand, *bytes.Buffer) {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	buffer := new(bytes.Buffer)
	baseCommand := &common.BaseCommand{
		Log: log,
		UI:  terminal.NewUI(context.TODO(), buffer),
	}

	c := &ReadCommand{
		BaseCommand: baseCommand,
	}
	c.init()
	return c, buffer
}
