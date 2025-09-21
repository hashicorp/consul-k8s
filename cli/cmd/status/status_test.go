// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package status

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmRelease "helm.sh/helm/v3/pkg/release"
	helmTime "helm.sh/helm/v3/pkg/time"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckConsulServers(t *testing.T) {
	namespace := "default"
	cases := map[string]struct {
		desired int
		healthy int
	}{
		"No servers":                    {},
		"3 servers expected, 1 healthy": {3, 1},
		"3 servers expected, 3 healthy": {3, 3},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := getInitializedCommand(t, buf)
			c.kubernetes = fake.NewSimpleClientset()

			// Deploy servers if needed.
			if name != "No servers" {
				err := createServers("consul-server", namespace, int32(tc.desired), int32(tc.healthy), c.kubernetes)
				require.NoError(t, err)
			}

			// Verify that the correct server statuses are seen.
			err := c.checkConsulServers(namespace)
			require.NoError(t, err)

			actual := buf.String()
			if tc.desired > 0 {
				require.Contains(t, actual, "NAME         \tREADY\tAGE          \tCONTAINERS\tIMAGES ")
				require.Contains(t, actual, "consul-server")
				require.Contains(t, actual, fmt.Sprintf("%d/%d", tc.healthy, tc.desired))
			} else {
				require.Contains(t, actual, "No Consul Server found in Kubernetes cluster.")
			}
			buf.Reset()
		})
	}
}
func TestCheckConsulClients(t *testing.T) {
	namespace := "default"
	cases := map[string]struct {
		desired int
		healthy int
	}{
		"No clients":                    {},
		"3 clients expected, 1 healthy": {3, 1},
		"3 clients expected, 3 healthy": {3, 3},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := getInitializedCommand(t, buf)
			c.kubernetes = fake.NewSimpleClientset()

			// Deploy clients if needed.
			if name != "No clients" {
				err := createClients("consul-client", namespace, int32(tc.desired), int32(tc.healthy), c.kubernetes)
				require.NoError(t, err)
			}

			// Verify that the correct server statuses are seen.
			err := c.checkConsulClients(namespace)
			require.NoError(t, err)

			actual := buf.String()
			if tc.desired > 0 {
				require.Contains(t, actual, "NAME         \tREADY\tAGE          \tCONTAINERS\tIMAGES ")
				require.Contains(t, actual, "consul-client")
				require.Contains(t, actual, fmt.Sprintf("%d/%d", tc.healthy, tc.desired))
			} else {
				require.Contains(t, actual, "No Consul Client found in Kubernetes cluster.")
			}
			buf.Reset()
		})
	}

}
func TestCheckConsulDeployments(t *testing.T) {
	namespace := "default"
	cases := map[string]struct {
		desired int
		healthy int
	}{
		"No deployments":                    {},
		"3 deployments expected, 1 healthy": {3, 1},
		"3 deployments expected, 3 healthy": {3, 3},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := getInitializedCommand(t, buf)
			c.kubernetes = fake.NewSimpleClientset()

			// Deploy clients if needed.
			if name != "No deployments" {
				err := createDeployments("consul-deployment", namespace, int32(tc.desired), int32(tc.healthy), c.kubernetes)
				require.NoError(t, err)
			}

			// Verify that the correct server statuses are seen.
			err := c.checkConsulDeployments(namespace)
			require.NoError(t, err)

			actual := buf.String()
			if tc.desired > 0 {
				require.Contains(t, actual, "NAME             \tREADY\tAGE          \tCONTAINERS\tIMAGES ")
				require.Contains(t, actual, "consul-deployment")
				require.Contains(t, actual, fmt.Sprintf("%d/%d", tc.healthy, tc.desired))
			} else {
				require.Contains(t, actual, "No Consul deployments found in Kubernetes cluster.")
			}
			buf.Reset()
		})
	}
}

// TestStatus creates a fake stateful set and tests the checkConsulServers function.
func TestStatus(t *testing.T) {
	nowTime := helmTime.Now()
	timezone, _ := nowTime.Zone()
	notImeStr := nowTime.Format("2006/01/02 15:04:05") + " " + timezone
	cases := map[string]struct {
		input              []string
		messages           []string
		preProcessingFunc  func(k8s kubernetes.Interface) error
		helmActionsRunner  *helm.MockActionRunner
		expectedReturnCode int
	}{
		"status with checkConsulComponentsStatus returns success": {
			input: []string{},
			messages: []string{
				fmt.Sprintf("\n==> Consul Status Summary\nName\tNamespace\tStatus\tChart Version\tAppVersion\tRevision\tLast Updated            \n    \t         \tREADY \t1.0.0        \t          \t0       \t%s\t\n", notImeStr),
				"\n==> Config:\n    {}\n",
				"\n==> Consul Client status:",
				"\nconsul-client-test1\t3/3",
				"\n==> Consul Server status:",
				"\nconsul-server-test1\t3/3",
				"\n==> Consul Deployment status:",
				"\nconsul-deployment-test1\t3/3",
			},
			preProcessingFunc: func(k8s kubernetes.Interface) error {
				if err := createServers("consul-server-test1", "consul", 3, 3, k8s); err != nil {
					return err
				}
				if err := createClients("consul-client-test1", "consul", 3, 3, k8s); err != nil {
					return err
				}
				if err := createDeployments("consul-deployment-test1", "consul", 3, 3, k8s); err != nil {
					return err
				}
				return nil
			},

			helmActionsRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*helmRelease.Release, error) {
					return &helmRelease.Release{
						Name: "consul", Namespace: "consul",
						Info: &helmRelease.Info{LastDeployed: nowTime, Status: "READY"},
						Chart: &chart.Chart{
							Metadata: &chart.Metadata{
								Version: "1.0.0",
							},
						},
						Config: make(map[string]interface{})}, nil
				},
			},
			expectedReturnCode: 0,
		},
		"status with pre-install and pre-upgrade hooks returns success and outputs hook status": {
			input: []string{},
			messages: []string{
				fmt.Sprintf("\n==> Consul Status Summary\nName\tNamespace\tStatus\tChart Version\tAppVersion\tRevision\tLast Updated            \n    \t         \tREADY \t1.0.0        \t          \t0       \t%s\t\n", notImeStr),
				"\n==> Config:\n    {}\n    \n",
				"\n==> Status Of Helm Hooks:\npre-install-hook pre-install: Succeeded\npre-upgrade-hook pre-upgrade: Succeeded",
				"\n==> Consul Client status:",
				"\nconsul-client-test1\t3/3",
				"\n==> Consul Server status:",
				"\nconsul-server-test1\t3/3",
				"\n==> Consul Deployment status:",
				"\nconsul-deployment-test1\t3/3",
			},
			preProcessingFunc: func(k8s kubernetes.Interface) error {
				if err := createServers("consul-server-test1", "consul", 3, 3, k8s); err != nil {
					return err
				}
				if err := createClients("consul-client-test1", "consul", 3, 3, k8s); err != nil {
					return err
				}
				if err := createDeployments("consul-deployment-test1", "consul", 3, 3, k8s); err != nil {
					return err
				}
				return nil
			},

			helmActionsRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*helmRelease.Release, error) {
					return &helmRelease.Release{
						Name: "consul", Namespace: "consul",
						Info: &helmRelease.Info{LastDeployed: nowTime, Status: "READY"},
						Chart: &chart.Chart{
							Metadata: &chart.Metadata{
								Version: "1.0.0",
							},
						},
						Config: make(map[string]interface{}),
						Hooks: []*helmRelease.Hook{
							{
								Name: "pre-install-hook",
								Kind: "pre-install", LastRun: helmRelease.HookExecution{
									Phase: helmRelease.HookPhaseSucceeded,
								},
								Events: []helmRelease.HookEvent{
									"pre-install",
								},
							},
							{
								Name: "pre-upgrade-hook",
								Kind: "pre-upgrade", LastRun: helmRelease.HookExecution{
									Phase: helmRelease.HookPhaseSucceeded,
								},
								Events: []helmRelease.HookEvent{
									"pre-install",
								},
							},
							{
								Name: "post-delete-hook",
								Kind: "post-delete", LastRun: helmRelease.HookExecution{
									Phase: helmRelease.HookPhaseSucceeded,
								},
								Events: []helmRelease.HookEvent{
									"post-delete",
								},
							},
						}}, nil
				},
			},
			expectedReturnCode: 0,
		},
		"status with CheckForInstallations error returns ": {
			input: []string{},
			messages: []string{
				"\n==> Consul Status Summary\n ! kaboom!\n",
			},
			preProcessingFunc: func(k8s kubernetes.Interface) error {
				return createServers("consul-server-test1", "consul", 3, 3, k8s)
			},

			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					return false, "", "", errors.New("kaboom!")
				},
			},
			expectedReturnCode: 1,
		},
		"status with GetStatus error returns ": {
			input: []string{},
			messages: []string{
				"\n==> Consul Status Summary\n ! couldn't check for installations: kaboom!\n",
			},
			preProcessingFunc: func(k8s kubernetes.Interface) error {
				return createServers("consul-server-test1", "consul", 3, 3, k8s)
			},

			helmActionsRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*helmRelease.Release, error) {
					return nil, errors.New("kaboom!")
				},
			},
			expectedReturnCode: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := getInitializedCommand(t, buf)
			c.kubernetes = fake.NewSimpleClientset()
			c.helmActionsRunner = tc.helmActionsRunner
			if tc.preProcessingFunc != nil {
				err := tc.preProcessingFunc(c.kubernetes)
				require.NoError(t, err)
			}
			returnCode := c.Run([]string{})
			require.Equal(t, tc.expectedReturnCode, returnCode)
			output := buf.String()
			for _, msg := range tc.messages {
				require.Contains(t, output, msg)
			}
		})
	}
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	cmd := getInitializedCommand(t, nil)

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
	cmd := getInitializedCommand(t, nil)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T, buf io.Writer) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})
	var ui terminal.UI
	if buf != nil {
		ui = terminal.NewUI(context.Background(), buf)
	} else {
		ui = terminal.NewBasicUI(context.Background())
	}
	baseCommand := &common.BaseCommand{
		Log: log,
		UI:  ui,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}

func createServers(name, namespace string, replicas, readyReplicas int32, k8s kubernetes.Interface) error {
	servers := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: readyReplicas,
		},
	}
	_, err := k8s.AppsV1().StatefulSets(namespace).Create(context.Background(), &servers, metav1.CreateOptions{})
	return err
}
func createClients(name, namespace string, replicas, readyReplicas int32, k8s kubernetes.Interface) error {
	clients := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "client"},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: replicas,
			NumberReady:            readyReplicas,
		},
	}
	_, err := k8s.AppsV1().DaemonSets(namespace).Create(context.Background(), &clients, metav1.CreateOptions{})
	return err
}
func createDeployments(name, namespace string, replicas, readyReplicas int32, k8s kubernetes.Interface) error {
	deployments := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "deployment"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      replicas,
			ReadyReplicas: readyReplicas,
		},
	}
	_, err := k8s.AppsV1().Deployments(namespace).Create(context.Background(), &deployments, metav1.CreateOptions{})
	return err
}
