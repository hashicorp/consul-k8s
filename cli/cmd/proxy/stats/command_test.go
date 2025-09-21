// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package stats

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFlagParsing(t *testing.T) {
	cases := map[string]struct {
		args []string
		out  int
	}{
		"No args, should fail": {
			args: []string{},
			out:  1,
		},
		"Nonexistent flag passed, -foo bar, should fail": {
			args: []string{"-foo", "bar"},
			out:  1,
		},
		"Invalid argument passed, -namespace notaname, should fail": {
			args: []string{"-namespace", "notaname"},
			out:  1,
		},
		"Valid podName passed, Invalid arg value(so it can fail at validation)": {
			args: []string{"podName", "-output", "image"},
			out:  1,
		},
		"Invalid argument passed, -output json, should fail": {
			args: []string{"-output", "json"},
			out:  1,
		},
		"Invalid argument passed, -namespace notaname -output pdf, should fail": {
			args: []string{"-namespace", "notaname", "-output", "pdf"},
			out:  1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := setupCommand(new(bytes.Buffer))
			c.kubernetes = fake.NewSimpleClientset()
			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}
func setupCommand(buf io.Writer) *StatsCommand {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	// Setup and initialize the command struct
	command := &StatsCommand{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()

	return command
}

type MockPortForwarder struct {
}

func (mpf *MockPortForwarder) Open(ctx context.Context) (string, error) {
	return "localhost:" + strconv.Itoa(envoyAdminPort), nil
}

func (mpf *MockPortForwarder) Close() {
	//noop
}

func (mpf *MockPortForwarder) GetLocalPort() int {
	return envoyAdminPort
}

func TestEnvoyStats(t *testing.T) {
	cases := map[string]struct {
		namespace string
		pods      []v1.Pod
	}{
		"Sidecar Pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
			},
		},
		"Pods in consul namespaces": {
			namespace: "consul",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-gateway",
						Namespace: "consul",
						Labels: map[string]string{
							"api-gateway.consul.hashicorp.com/managed": "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "consul",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
			},
		},
	}

	srv := startHttpServer(envoyAdminPort)
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := setupCommand(new(bytes.Buffer))
			c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: tc.pods})
			c.flagNamespace = tc.namespace
			for _, pod := range tc.pods {
				c.flagPod = pod.Name
				mpf := &MockPortForwarder{}
				resp, err := c.getEnvoyStats(mpf)
				require.NoError(t, err)
				require.Equal(t, resp, "Envoy Stats")
			}
		})
	}
	srv.Shutdown(context.Background())
}

func startHttpServer(port int) *http.Server {
	srv := &http.Server{Addr: ":" + strconv.Itoa(port)}

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Envoy Stats")
	})

	go func() {
		srv.ListenAndServe()
	}()

	return srv
}
func TestCaptureEnvoyStats(t *testing.T) {
	mockJSONResponse := `{"server":{"stats_recent_lookups":0}}`
	// indentation should match the output of json.MarshalIndent in command.go
	expectedFileContent := `{
	"server": {
		"stats_recent_lookups": 0
	}
}`

	cases := map[string]struct {
		namespace string
		pods      []v1.Pod
	}{
		"Sidecar Pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
			},
		},
		"Pods in consul namespace": {
			namespace: "consul",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-gateway",
						Namespace: "consul",
						Labels: map[string]string{
							"api-gateway.consul.hashicorp.com/managed": "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "consul",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
			},
		},
	}

	srv := startHttpServerForCapture(envoyAdminPort, mockJSONResponse)
	defer srv.Shutdown(context.Background())

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Create a temporary directory for this test run.
			tempDir := t.TempDir()

			// Change the current working directory to our temporary directory.
			originalWD, err := os.Getwd()
			require.NoError(t, err)
			err = os.Chdir(tempDir)
			require.NoError(t, err)
			defer os.Chdir(originalWD) // Ensure we change back.

			c := setupCommand(new(bytes.Buffer))
			c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: tc.pods})
			c.flagNamespace = tc.namespace

			for _, pod := range tc.pods {
				c.flagPod = pod.Name
				mpf := &MockPortForwarder{}

				expectedFilePath := filepath.Join(tempDir, "proxy", "proxy-stats-"+pod.Name+".json")
				err := c.captureEnvoyStats(mpf, expectedFilePath)

				require.NoError(t, err)

				_, err = os.Stat(expectedFilePath)
				require.NoError(t, err, "expected output file '%s' to be created, but it was not", expectedFilePath)

				actualFileContent, err := os.ReadFile(expectedFilePath)
				require.NoError(t, err)
				require.Equal(t, expectedFileContent, string(actualFileContent))
			}
		})
	}
}
func startHttpServerForCapture(port int, jsonResponse string) *http.Server {
	srv := &http.Server{Addr: ":" + strconv.Itoa(port)}

	handler := http.NewServeMux()
	handler.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		// Check if the `format=json` query parameter is present.
		if r.URL.Query().Get("format") == "json" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, jsonResponse)
		} else {
			http.Error(w, "format must be json", http.StatusBadRequest)
		}
	})
	srv.Handler = handler

	go func() {
		srv.ListenAndServe()
	}()

	return srv
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	cmd := setupCommand(buf)

	predictor := cmd.AutocompleteFlags()

	// Test that we get the expected number of predictions
	args := complete.Args{Last: "-"}
	res := predictor.Predict(args)

	// Grab the list of flags from the Flag object
	flags := make([]string, 0)
	cmd.set.VisitSets(func(name string, set *cmnFlag.Set) {
		set.VisitAll(func(flag *flag.Flag) {
			flags = append(flags, fmt.Sprintf("-%s", flag.Name))
		})
	})

	// Verify that there is a prediction for each flag associated with the command
	assert.Equal(t, len(flags), len(res))
	assert.ElementsMatch(t, flags, res, "flags and predictions didn't match, make sure to add "+
		"new flags to the command AutoCompleteFlags function")
}
func TestTaskCreateCommand_AutocompleteArgs(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := setupCommand(buf)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}

// func TestProxyStatsRunFunc(t *testing.T) {
// 	pods := []v1.Pod{
// 		{
// 			ObjectMeta: metav1.ObjectMeta{
// 				Name:      "sidecar-pod",
// 				Namespace: "default",
// 				Labels: map[string]string{
// 					"consul.hashicorp.com/connect-inject-status": "injected",
// 				},
// 			},
// 		},
// 	}
// 	cases := map[string]struct {
// 		namespace          string
// 		pods               []v1.Pod
// 		args               []string
// 		expectedReturnCode int
// 	}{
// 		"no args, should fail": {
// 			namespace:          "default",
// 			pods:               pods,
// 			args:               []string{},
// 			expectedReturnCode: 1,
// 		},
// 		"valid args, should pass": {
// 			namespace:          "default",
// 			pods:               pods,
// 			args:               []string{"sidecar-pod", "-namespace", "default"},
// 			expectedReturnCode: 0,
// 		},
// 		"invalid args, should fail": {
// 			namespace:          "consul",
// 			pods:               pods,
// 			args:               []string{"sidecar-pod", "-namespace", "notanamespace"},
// 			expectedReturnCode: 1,
// 		},
// 		"valid args with output, should pass": {
// 			namespace:          "consul",
// 			pods:               pods,
// 			args:               []string{"sidecar-pod", "-namespace", "default", "-output", "archive"},
// 			expectedReturnCode: 0,
// 		},
// 	}

// 	mockJSONResponse := `{"server":{"stats_recent_lookups":0}}`
// 	// indentation should match the output of json.MarshalIndent in command.go
// 	expectedFileContent := `{
// 	"server": {
// 		"stats_recent_lookups": 0
// 	}
// }`
// 	srv1 := startHttpServerForCapture(envoyAdminPort, mockJSONResponse)
// 	defer srv1.Shutdown(context.Background())
// 	srv2 := startHttpServer(envoyAdminPort)
// 	defer srv2.Shutdown(context.Background())

// 	for name, tc := range cases {
// 		t.Run(name, func(t *testing.T) {
// 			// Create a temporary directory for this test run.
// 			tempDir := t.TempDir()

// 			// Change the current working directory to our temporary directory.
// 			originalWD, err := os.Getwd()
// 			require.NoError(t, err)
// 			err = os.Chdir(tempDir)
// 			require.NoError(t, err)
// 			defer os.Chdir(originalWD) // Ensure we change back.

// 			// cmd.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{pods[0]}})
// 			buf := new(bytes.Buffer)
// 			c := setupCommand(buf)
// 			c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: tc.pods})
// 			c.flagNamespace = tc.namespace
// 			returnCode := c.Run(tc.args)
// 			require.Equal(t, tc.expectedReturnCode, returnCode)

// 			if tc.expectedReturnCode == 0 {
// 				if slices.Contains(tc.args, "archive") {
// 					expectedFilePath := filepath.Join(tempDir, "proxy", "proxy-stats-"+tc.args[0]+".json")
// 					_, err = os.Stat(expectedFilePath)
// 					require.NoError(t, err, "expected output file '%s' to be created, but it was not", expectedFilePath)

// 					actualFileContent, err := os.ReadFile(expectedFilePath)
// 					require.NoError(t, err)
// 					require.Equal(t, expectedFileContent, string(actualFileContent))
// 				} else {
// 					output := buf.String()
// 					require.Contains(t, output, "Envoy Stats")
// 				}
// 			}
// 		})
// 	}
// }
