// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package stats

import (
	"bytes"
	"context"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"io"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"os"
	"strconv"
	"testing"
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
