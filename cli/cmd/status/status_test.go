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

func TestCheckConsulAgents(t *testing.T) {
	namespace := "default"

	cases := map[string]struct {
		clients int
		servers int
	}{
		"No clients, no agents": {0, 0},
		"3 clients, no agents":  {3, 0},
		"3 clients, 3 agents":   {3, 3},
		"No clients, 3 agents":  {3, 3},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := setupCommand(buf)
			c.kubernetes = fake.NewSimpleClientset()

			// Before deployment, we shouldn't see the checks or errors.
			err := c.checkConsulAgents(namespace)
			require.NoError(t, err)

			actual := buf.String()
			require.NotContains(t, actual, "Consul Clients Healthy")
			require.NotContains(t, actual, "Consul Servers Healthy")
			buf.Reset()

			if tc.clients != 0 {
				// Deploy clients where only 1 of the clients is healthy.
				ds := &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consul-clients",
						Namespace: namespace,
						Labels:    map[string]string{"app": "consul", "chart": "consul-helm"},
					},
					Status: appsv1.DaemonSetStatus{
						DesiredNumberScheduled: int32(tc.clients),
						NumberReady:            1,
					},
				}
				c.kubernetes.AppsV1().DaemonSets(namespace).Create(context.Background(), ds, metav1.CreateOptions{})
			}

			if tc.servers != 0 {
				// Deploy servers where only 1 of the servers is healthy.
				servers := int32(tc.servers)
				ss := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: namespace,
						Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: &servers,
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      int32(tc.servers),
						ReadyReplicas: 1,
					},
				}
				c.kubernetes.AppsV1().StatefulSets(namespace).Create(context.Background(), ss, metav1.CreateOptions{})
			}

			// Verify that unhealthy clients and servers are seen.
			err = c.checkConsulAgents(namespace)
			require.NoError(t, err)

			actual = buf.String()
			if tc.clients != 0 {
				require.Contains(t, actual, fmt.Sprintf("Consul Clients Healthy 1/%d", tc.clients))
			}
			if tc.servers != 0 {
				require.Contains(t, actual, fmt.Sprintf("Consul Servers Healthy 1/%d", tc.servers))
			}
			buf.Reset()

			if tc.clients != 0 {
				// Update clients so that all clients are healthy.
				ds := &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consul-clients",
						Namespace: namespace,
						Labels:    map[string]string{"app": "consul", "chart": "consul-helm"},
					},
					Status: appsv1.DaemonSetStatus{
						DesiredNumberScheduled: int32(tc.clients),
						NumberReady:            int32(tc.clients),
					},
				}
				c.kubernetes.AppsV1().DaemonSets(namespace).Update(context.Background(), ds, metav1.UpdateOptions{})
			}

			if tc.servers != 0 {
				// Update servers so that all servers are healthy.
				servers := int32(tc.servers)
				ss := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: namespace,
						Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: &servers,
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      int32(tc.servers),
						ReadyReplicas: int32(tc.servers),
					},
				}
				c.kubernetes.AppsV1().StatefulSets(namespace).Update(context.Background(), ss, metav1.UpdateOptions{})
			}

			// Verify that healthy clients and servers are seen.
			err = c.checkConsulAgents(namespace)
			require.NoError(t, err)

			actual = buf.String()
			if tc.clients != 0 {
				require.Contains(t, actual, fmt.Sprintf("Consul Clients Healthy %d/%d", tc.clients, tc.clients))
			}
			if tc.servers != 0 {
				require.Contains(t, actual, fmt.Sprintf("Consul Servers Healthy %d/%d", tc.servers, tc.servers))
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
		preProcessingFunc  func(k8s kubernetes.Interface)
		helmActionsRunner  *helm.MockActionRunner
		expectedReturnCode int
	}{
		"status with clients and servers returns success": {
			input: []string{},
			messages: []string{
				fmt.Sprintf("\n==> Consul Status Summary\nName\tNamespace\tStatus\tChart Version\tAppVersion\tRevision\tLast Updated            \n    \t         \tREADY \t1.0.0        \t          \t0       \t%s\t\n", notImeStr),
				"\n==> Config:\n    {}\n    \nConsul Clients Healthy 3/3\nConsul Servers Healthy 3/3\n",
			},
			preProcessingFunc: func(k8s kubernetes.Interface) {
				createDaemonset("consul-client-test1", "consul", 3, 3, k8s)
				createStatefulSet("consul-server-test1", "consul", 3, 3, k8s)
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
				"\n==> Status Of Helm Hooks:\npre-install-hook pre-install: Succeeded\npre-upgrade-hook pre-upgrade: Succeeded\nConsul Servers Healthy 3/3\n",
			},
			preProcessingFunc: func(k8s kubernetes.Interface) {
				createStatefulSet("consul-server-test1", "consul", 3, 3, k8s)
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
			preProcessingFunc: func(k8s kubernetes.Interface) {
				createDaemonset("consul-client-test1", "consul", 3, 3, k8s)
				createStatefulSet("consul-server-test1", "consul", 3, 3, k8s)
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
			preProcessingFunc: func(k8s kubernetes.Interface) {
				createDaemonset("consul-client-test1", "consul", 3, 3, k8s)
				createStatefulSet("consul-server-test1", "consul", 3, 3, k8s)
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
				tc.preProcessingFunc(c.kubernetes)
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

func createStatefulSet(name, namespace string, replicas, readyReplicas int32, k8s kubernetes.Interface) {
	ss := &appsv1.StatefulSet{
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

	k8s.AppsV1().StatefulSets(namespace).Create(context.Background(), ss, metav1.CreateOptions{})
}

func createDaemonset(name, namespace string, replicas, readyReplicas int32, k8s kubernetes.Interface) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm"},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: replicas,
			NumberReady:            readyReplicas,
		},
	}

	k8s.AppsV1().DaemonSets(namespace).Create(context.Background(), ds, metav1.CreateOptions{})
}

func setupCommand(buf io.Writer) *Command {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	// Setup and initialize the command struct
	command := &Command{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()

	return command
}
