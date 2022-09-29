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

// TestCheckConsulServers creates a fake stateful set and tests the checkConsulServers function.
func TestCheckConsulServers(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.kubernetes = fake.NewSimpleClientset()

	// First check that no stateful sets causes an error.
	_, err := c.checkConsulServers("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no server stateful set found")

	// Next create a stateful set with 3 desired replicas and 3 ready replicas.
	var replicas int32 = 3

	createStatefulSet("consul-server-test1", "default", replicas, replicas, c.kubernetes)

	// Now we run the checkConsulServers() function and it should succeed.
	s, err := c.checkConsulServers("default")
	require.NoError(t, err)
	require.Equal(t, "Consul servers healthy (3/3)", s)

	// If you then create another stateful set it should error.
	createStatefulSet("consul-server-test2", "default", replicas, replicas, c.kubernetes)
	_, err = c.checkConsulServers("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "found multiple server stateful sets")

	// Clear out the client and now run a test where the stateful set isn't ready.
	c.kubernetes = fake.NewSimpleClientset()
	createStatefulSet("consul-server-test2", "default", replicas, replicas-1, c.kubernetes)

	_, err = c.checkConsulServers("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("%d/%d Consul servers unhealthy", 1, replicas))
}

// TestCheckConsulClients is very similar to TestCheckConsulServers() in structure.
func TestCheckConsulClients(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.kubernetes = fake.NewSimpleClientset()

	// No client daemon set should cause an error.
	_, err := c.checkConsulClients("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no client daemon set found")

	// Next create a daemon set.
	var desired int32 = 3

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-client-test1",
			Namespace: "default",
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm"},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: desired,
			NumberReady:            desired,
		},
	}

	c.kubernetes.AppsV1().DaemonSets("default").Create(context.Background(), ds, metav1.CreateOptions{})

	// Now run checkConsulClients() and make sure it succeeds.
	s, err := c.checkConsulClients("default")
	require.NoError(t, err)
	require.Equal(t, "Consul clients healthy (3/3)", s)

	// Creating another daemon set should cause an error.
	ds2 := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-client-test2",
			Namespace: "default",
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm"},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: desired,
			NumberReady:            desired,
		},
	}
	c.kubernetes.AppsV1().DaemonSets("default").Create(context.Background(), ds2, metav1.CreateOptions{})

	_, err = c.checkConsulClients("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "found multiple client daemon sets")

	// Clear out the client and run a test with fewer than desired daemon sets ready.
	c.kubernetes = fake.NewSimpleClientset()

	ds3 := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-client-test2",
			Namespace: "default",
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm"},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: desired,
			NumberReady:            desired - 1,
		},
	}
	c.kubernetes.AppsV1().DaemonSets("default").Create(context.Background(), ds3, metav1.CreateOptions{})

	_, err = c.checkConsulClients("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("%d/%d Consul clients unhealthy", 1, desired))
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
				"\n==> Config:\n    {}\n    \n ✓ Consul servers healthy (3/3)\n ✓ Consul clients healthy (3/3)\n",
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
		"status with no servers returns error": {
			input: []string{},
			messages: []string{
				fmt.Sprintf("\n==> Consul Status Summary\nName\tNamespace\tStatus\tChart Version\tAppVersion\tRevision\tLast Updated            \n    \t         \tREADY \t1.0.0        \t          \t0       \t%s\t\n", notImeStr),
				"\n==> Config:\n    {}\n    \n ! no server stateful set found\n",
			},
			preProcessingFunc: func(k8s kubernetes.Interface) {
				createDaemonset("consul-client-test1", "consul", 3, 3, k8s)
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
			expectedReturnCode: 1,
		},
		"status with no clients returns error": {
			input: []string{},
			messages: []string{
				fmt.Sprintf("\n==> Consul Status Summary\nName\tNamespace\tStatus\tChart Version\tAppVersion\tRevision\tLast Updated            \n    \t         \tREADY \t1.0.0        \t          \t0       \t%s\t\n", notImeStr),
				"\n==> Config:\n    {}\n    \n ✓ Consul servers healthy (3/3)\n ! no client daemon set found\n",
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
						Config: make(map[string]interface{})}, nil
				},
			},
			expectedReturnCode: 1,
		},
		"status with pre-install and pre-upgrade hooks returns success and outputs hook status": {
			input: []string{},
			messages: []string{
				fmt.Sprintf("\n==> Consul Status Summary\nName\tNamespace\tStatus\tChart Version\tAppVersion\tRevision\tLast Updated            \n    \t         \tREADY \t1.0.0        \t          \t0       \t%s\t\n", notImeStr),
				"\n==> Config:\n    {}\n    \n",
				"\n==> Status Of Helm Hooks:\npre-install-hook pre-install: Succeeded\npre-upgrade-hook pre-upgrade: Succeeded\n ✓ Consul servers healthy (3/3)\n ✓ Consul clients healthy (3/3)\n",
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
