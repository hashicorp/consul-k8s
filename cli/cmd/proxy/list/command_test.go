// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package list

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

func TestFlagParsing(t *testing.T) {
	cases := map[string]struct {
		args []string
		out  int
	}{
		"No args": {
			args: []string{},
			out:  0,
		},
		"Nonexistent flag passed, -foo bar": {
			args: []string{"-foo", "bar"},
			out:  1,
		},
		"Invalid argument passed, -namespace YOLO": {
			args: []string{"-namespace", "YOLO"},
			out:  1,
		},
		"Invalid argument passed, -output-format pdf ": {
			args: []string{"-namespace", "YOLO"},
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

func TestFetchPods(t *testing.T) {
	cases := map[string]struct {
		namespace    string
		pods         []v1.Pod
		expectedPods int
	}{
		"No pods": {
			namespace:    "default",
			pods:         []v1.Pod{},
			expectedPods: 0,
		},
		"Gateway pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "ingress-gateway",
							"chart":     "consul-helm",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mesh-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "mesh-gateway",
							"chart":     "consul-helm",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "terminating-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "terminating-gateway",
							"chart":     "consul-helm",
						},
					},
				},
			},
			expectedPods: 3,
		},
		"API Gateway Pods": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"api-gateway.consul.hashicorp.com/managed": "true",
						},
					},
				},
			},
			expectedPods: 1,
		},
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
			expectedPods: 1,
		},
		"All kinds of Pods": {
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mesh-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"component": "mesh-gateway",
							"chart":     "consul-helm",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "api-gateway",
						Namespace: "default",
						Labels: map[string]string{
							"api-gateway.consul.hashicorp.com/managed": "true",
						},
					},
				},
			},
			expectedPods: 3,
		},
		"Pods in multiple namespaces": {
			namespace: "",
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
						Namespace: "default",
						Labels: map[string]string{
							"consul.hashicorp.com/connect-inject-status": "injected",
						},
					},
				},
			},
			expectedPods: 2,
		},
		"Pods which should not be fetched": {
			namespace: "default",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dont-fetch",
						Namespace: "default",
						Labels:    map[string]string{},
					},
				},
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
			expectedPods: 1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := setupCommand(new(bytes.Buffer))
			c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: tc.pods})
			c.flagNamespace = tc.namespace
			if tc.namespace == "" {
				c.flagAllNamespaces = true
			}

			pods, err := c.fetchPods()

			require.NoError(t, err)
			require.Equal(t, tc.expectedPods, len(pods))
		})
	}
}

// TestListCommandOutput tests the output of the list command. The output must
// contain the expected regular expressions and not contain the not expected
// regular expressions.
func TestListCommandOutput(t *testing.T) {
	// These regular expressions must be present in the output.
	expected := []string{
		"Namespace.*Name.*Type",
		"consul.*mesh-gateway.*Mesh Gateway",
		"consul.*terminating-gateway.*Terminating Gateway",
		"default.*ingress-gateway.*Ingress Gateway",
		"consul.*api-gateway.*API Gateway",
		"default.*pod1.*Sidecar",
	}
	notExpected := []string{
		"default.*dont-fetch.*Sidecar",
	}

	pods := []v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingress-gateway",
				Namespace: "default",
				Labels: map[string]string{
					"component": "ingress-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mesh-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component": "mesh-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "terminating-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component": "terminating-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "depricated-api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"api-gateway.consul.hashicorp.com/managed": "true",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component": "api-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "both-labels-api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"api-gateway.consul.hashicorp.com/managed": "true",
					"component": "api-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dont-fetch",
				Namespace: "default",
				Labels:    map[string]string{},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
				Labels: map[string]string{
					"consul.hashicorp.com/connect-inject-status": "injected",
				},
			},
		},
	}
	client := fake.NewSimpleClientset(&v1.PodList{Items: pods})

	buf := new(bytes.Buffer)
	c := setupCommand(buf)
	c.kubernetes = client

	out := c.Run([]string{"-A"})
	require.Equal(t, 0, out)

	actual := buf.String()

	for _, expression := range expected {
		require.Regexp(t, expression, actual)
	}
	for _, expression := range notExpected {
		require.NotRegexp(t, expression, actual)
	}
}

// TestListCommandOutputInJsonFormat tests the output of the list command when
// the output format is set to JSON. The proxies must be presented in the appropriate
// order and format.
func TestListCommandOutputInJsonFormat(t *testing.T) {
	pods := []v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingress-gateway",
				Namespace: "default",
				Labels: map[string]string{
					"component": "ingress-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mesh-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component": "mesh-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "terminating-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component": "terminating-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component":                            "api-gateway",
					"gateway.consul.hashicorp.com/managed": "true",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "both-labels-api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"api-gateway.consul.hashicorp.com/managed": "true",
					"component": "api-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deprecated-api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"api-gateway.consul.hashicorp.com/managed": "true",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dont-fetch",
				Namespace: "default",
				Labels:    map[string]string{},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
				Labels: map[string]string{
					"consul.hashicorp.com/connect-inject-status": "injected",
				},
			},
		},
	}
	client := fake.NewSimpleClientset(&v1.PodList{Items: pods})

	buf := new(bytes.Buffer)
	c := setupCommand(buf)
	c.kubernetes = client

	out := c.Run([]string{"-A", "-o", "json"})
	require.Equal(t, 0, out)

	var actual []struct {
		Name      string `json:"Name"`
		Namespace string `json:"Namespace"`
		Type      string `json:"Type"`
	}
	require.NoErrorf(t, json.Unmarshal(buf.Bytes(), &actual), "failed to parse json output: %s", buf.String())

	require.Len(t, actual, 7)
	assert.Equal(t, "api-gateway", actual[0].Name)
	assert.Equal(t, "both-labels-api-gateway", actual[1].Name)
	assert.Equal(t, "deprecated-api-gateway", actual[2].Name)
	assert.Equal(t, "ingress-gateway", actual[3].Name)
	assert.Equal(t, "mesh-gateway", actual[4].Name)
	assert.Equal(t, "terminating-gateway", actual[5].Name)
	assert.Equal(t, "pod1", actual[6].Name)
}
func TestListCommandOutputInArchiveFormat(t *testing.T) {
	pods := []v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingress-gateway",
				Namespace: "default",
				Labels: map[string]string{
					"component": "ingress-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mesh-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component": "mesh-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "terminating-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component": "terminating-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"component":                            "api-gateway",
					"gateway.consul.hashicorp.com/managed": "true",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "both-labels-api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"api-gateway.consul.hashicorp.com/managed": "true",
					"component": "api-gateway",
					"chart":     "consul-helm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deprecated-api-gateway",
				Namespace: "consul",
				Labels: map[string]string{
					"api-gateway.consul.hashicorp.com/managed": "true",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dont-fetch",
				Namespace: "default",
				Labels:    map[string]string{},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
				Labels: map[string]string{
					"consul.hashicorp.com/connect-inject-status": "injected",
				},
			},
		},
	}
	client := fake.NewSimpleClientset(&v1.PodList{Items: pods})

	// Create a temporary directory for this test run.
	tempDir := t.TempDir()

	// Change the current working directory to our temporary directory.
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tempDir)
	require.NoError(t, err)
	defer os.Chdir(originalWD) // Ensure we change back.

	buf := new(bytes.Buffer)
	c := setupCommand(buf)
	c.kubernetes = client

	out := c.Run([]string{"-A", "-o", "archive"})
	require.Equal(t, 0, out)

	expectedFilePath := filepath.Join(tempDir, "proxy", "proxy-list.json")
	_, err = os.Stat(expectedFilePath)
	require.NoError(t, err, "expected output file to be created, but it was not")

	actualJSON, err := os.ReadFile(expectedFilePath)
	require.NoError(t, err)

	var actual []struct {
		Name      string `json:"Name"`
		Namespace string `json:"Namespace"`
		Type      string `json:"Type"`
	}
	require.NoErrorf(t, json.Unmarshal(actualJSON, &actual), "failed to parse json output: %s", actualJSON)

	require.Len(t, actual, 7)
	assert.Equal(t, "api-gateway", actual[0].Name)
	assert.Equal(t, "both-labels-api-gateway", actual[1].Name)
	assert.Equal(t, "deprecated-api-gateway", actual[2].Name)
	assert.Equal(t, "ingress-gateway", actual[3].Name)
	assert.Equal(t, "mesh-gateway", actual[4].Name)
	assert.Equal(t, "terminating-gateway", actual[5].Name)
	assert.Equal(t, "pod1", actual[6].Name)
}

func TestNoPodsFound(t *testing.T) {
	cases := map[string]struct {
		args     []string
		expected string
	}{
		"Default namespace": {
			[]string{"-n", "default"},
			"No proxies found in default namespace.",
		},
		"All namespaces": {
			[]string{"-A"},
			"No proxies found across all namespaces.",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := setupCommand(buf)
			c.kubernetes = fake.NewSimpleClientset()

			exitCode := c.Run(tc.args)
			require.Equal(t, 0, exitCode)

			require.Contains(t, buf.String(), tc.expected)
		})
	}
}

func setupCommand(buf io.Writer) *ListCommand {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	// Setup and initialize the command struct
	command := &ListCommand{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()

	return command
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
