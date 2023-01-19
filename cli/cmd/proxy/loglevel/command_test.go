package loglevel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFlagParsing(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		args []string
		out  int
	}{
		"No args": {
			args: []string{},
			out:  1,
		},
		"With pod name": {
			args: []string{"now-this-is-pod-racing"},
			out:  0,
		},
	}
	podName := "now-this-is-pod-racing"
	fakePod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: "default",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			c := setupCommand(bytes.NewBuffer([]byte{}))
			c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{fakePod}})
			c.logLevelFetcher = func(context.Context, common.PortForwarder) (LoggerConfig, error) {
				return testLogConfig, nil
			}

			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

func TestOutputForGettingLogLevel(t *testing.T) {
	t.Parallel()
	podName := "now-this-is-pod-racing"
	expectedHeader := fmt.Sprintf("Envoy log configuration for %s in namespace default:", podName)
	fakePod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: "default",
		},
	}

	buf := bytes.NewBuffer([]byte{})
	c := setupCommand(buf)
	c.logLevelFetcher = func(context.Context, common.PortForwarder) (LoggerConfig, error) {
		return testLogConfig, nil
	}
	c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{fakePod}})

	args := []string{podName}
	out := c.Run(args)
	require.Equal(t, 0, out)

	actual := buf.String()

	require.Regexp(t, expectedHeader, actual)
	require.Regexp(t, "Log Levels for now-this-is-pod-racing", actual)
	for logger, level := range testLogConfig {
		require.Regexp(t, regexp.MustCompile(logger+`\s*`+level), actual)
	}
}

func TestHelp(t *testing.T) {
	buf := bytes.NewBuffer([]byte{})
	c := setupCommand(buf)
	expectedSynposis := "Inspect and Modify the Envoy Log configuration for a given Pod."
	expectedUsage := `Usage: consul-k8s proxy log <pod-name> \[flags\]`
	actual := c.Help()
	require.Regexp(t, expectedSynposis, actual)
	require.Regexp(t, expectedUsage, actual)
}

func TestFetchLogLevel(t *testing.T) {
	rawLogLevels, err := os.ReadFile("testdata/fetch_debug_levels.txt")
	require.NoError(t, err)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rawLogLevels)
	}))

	defer mockServer.Close()

	mpf := &mockPortForwarder{
		openBehavior: func(ctx context.Context) (string, error) {
			return strings.Replace(mockServer.URL, "http://", "", 1), nil
		},
	}
	logLevels, err := FetchLogLevel(context.Background(), mpf)
	require.NoError(t, err)
	require.Equal(t, testLogConfig, logLevels)
}

type mockPortForwarder struct {
	openBehavior func(context.Context) (string, error)
}

func (m *mockPortForwarder) Open(ctx context.Context) (string, error) { return m.openBehavior(ctx) }
func (m *mockPortForwarder) Close()                                   {}

var testLogConfig = LoggerConfig{
	"admin":                     "debug",
	"alternate_protocols_cache": "debug",
	"aws":                       "debug",
	"assert":                    "debug",
	"backtrace":                 "debug",
	"cache_filter":              "debug",
	"client":                    "debug",
	"config":                    "debug",
	"connection":                "debug",
	"conn_handler":              "debug",
	"decompression":             "debug",
	"dns":                       "debug",
	"dubbo":                     "debug",
	"envoy_bug":                 "debug",
	"ext_authz":                 "debug",
	"ext_proc":                  "debug",
	"rocketmq":                  "debug",
	"file":                      "debug",
	"filter":                    "debug",
	"forward_proxy":             "debug",
	"grpc":                      "debug",
	"happy_eyeballs":            "debug",
	"hc":                        "debug",
	"health_checker":            "debug",
	"http":                      "debug",
	"http2":                     "debug",
	"hystrix":                   "debug",
	"init":                      "debug",
	"io":                        "debug",
	"jwt":                       "debug",
	"kafka":                     "debug",
	"key_value_store":           "debug",
	"lua":                       "debug",
	"main":                      "debug",
	"matcher":                   "debug",
	"misc":                      "debug",
	"mongo":                     "debug",
	"multi_connection":          "debug",
	"oauth2":                    "debug",
	"quic":                      "debug",
	"quic_stream":               "debug",
	"pool":                      "debug",
	"rbac":                      "debug",
	"rds":                       "debug",
	"redis":                     "debug",
	"router":                    "debug",
	"runtime":                   "debug",
	"stats":                     "debug",
	"secret":                    "debug",
	"tap":                       "debug",
	"testing":                   "debug",
	"thrift":                    "debug",
	"tracing":                   "debug",
	"upstream":                  "debug",
	"udp":                       "debug",
	"wasm":                      "debug",
	"websocket":                 "debug",
}

func setupCommand(buf io.Writer) *LogCommand {
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	command := &LogCommand{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()
	return command
}
