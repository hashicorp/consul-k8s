package loglevel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/envoy"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
)

func TestFlagParsingFails(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
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
			c.envoyLoggingCaller = func(context.Context, common.PortForwarder, *envoy.LoggerParams) (map[string]string, error) {
				return testLogConfig, nil
			}

			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

func TestFlagParsingSucceeds(t *testing.T) {
	t.Parallel()
	podName := "now-this-is-pod-racing"
	testCases := map[string]struct {
		args         []string
		podNamespace string
		out          int
	}{
		"With single pod name": {
			args:         []string{podName},
			podNamespace: "default",
			out:          0,
		},
		"With single pod name and namespace": {
			args:         []string{podName, "-n", "another"},
			podNamespace: "another",
			out:          0,
		},
		"With single pod name and blanket level": {
			args:         []string{podName, "-u", "warning"},
			podNamespace: "default",
			out:          0,
		},
		"With single pod name and single level": {
			args:         []string{podName, "-u", "grpc:warning"},
			podNamespace: "default",
			out:          0,
		},
		"With single pod name and multiple levels": {
			args:         []string{podName, "-u", "grpc:warning,http:info"},
			podNamespace: "default",
			out:          0,
		},
		"With single pod name and blanket level full flag": {
			args:         []string{podName, "-update-level", "warning"},
			podNamespace: "default",
			out:          0,
		},
		"With single pod name and single level full flag": {
			args:         []string{podName, "-update-level", "grpc:warning"},
			podNamespace: "default",
			out:          0,
		},
		"With single pod name and multiple levels full flag": {
			args:         []string{podName, "-update-level", "grpc:warning,http:info"},
			podNamespace: "default",
			out:          0,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			fakePod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: tc.podNamespace,
				},
			}

			c := setupCommand(bytes.NewBuffer([]byte{}))
			c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{fakePod}})
			c.envoyLoggingCaller = func(context.Context, common.PortForwarder, *envoy.LoggerParams) (map[string]string, error) {
				return testLogConfig, nil
			}

			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

func TestOutputForGettingLogLevels(t *testing.T) {
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
	newLogLevel := "warning"
	config := make(map[string]string, len(testLogConfig))
	for logger := range testLogConfig {
		config[logger] = newLogLevel
	}

	c.envoyLoggingCaller = func(context.Context, common.PortForwarder, *envoy.LoggerParams) (map[string]string, error) {
		return config, nil
	}
	c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{fakePod}})

	args := []string{podName, "-u", newLogLevel}
	out := c.Run(args)
	require.Equal(t, 0, out)

	actual := buf.String()

	require.Regexp(t, expectedHeader, actual)
	require.Regexp(t, "Log Levels for now-this-is-pod-racing", actual)
	for logger, level := range config {
		require.Regexp(t, regexp.MustCompile(logger+`.*`+level), actual)
	}
}

func TestOutputForSettingLogLevels(t *testing.T) {
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
	c.envoyLoggingCaller = func(context.Context, common.PortForwarder, *envoy.LoggerParams) (map[string]string, error) {
		return testLogConfig, nil
	}
	c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{fakePod}})

	args := []string{podName, "-u", "warning"}
	out := c.Run(args)
	require.Equal(t, 0, out)

	actual := buf.String()

	require.Regexp(t, expectedHeader, actual)
	require.Regexp(t, "Log Levels for now-this-is-pod-racing", actual)
	for logger, level := range testLogConfig {
		require.Regexp(t, regexp.MustCompile(logger+`.*`+level), actual)
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()
	buf := bytes.NewBuffer([]byte{})
	c := setupCommand(buf)
	expectedSynposis := "Inspect and Modify the Envoy Log configuration for a given Pod."
	expectedUsage := `Usage: consul-k8s proxy log <pod-name> \[flags\]`
	actual := c.Help()
	require.Regexp(t, expectedSynposis, actual)
	require.Regexp(t, expectedUsage, actual)
}

func setupCommand(buf io.Writer) *LogLevelCommand {
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	command := &LogLevelCommand{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()
	return command
}

var testLogConfig = map[string]string{
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
