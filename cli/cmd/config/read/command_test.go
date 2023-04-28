package read

import (
	"bytes"
	"context"
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

func TestConfigRead(t *testing.T) {
	nowTime := helmTime.Now()
	cases := map[string]struct {
		messages           []string
		helmActionsRunner  *helm.MockActionRunner
		expectedReturnCode int
	}{
		"empty config": {
			messages: []string{"\n"},

			helmActionsRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*helmRelease.Release, error) {
					return &helmRelease.Release{
						Name: "consul", Namespace: "consul",
						Info:   &helmRelease.Info{LastDeployed: nowTime, Status: "READY"},
						Chart:  &chart.Chart{Metadata: &chart.Metadata{Version: "1.0.0"}},
						Config: make(map[string]interface{})}, nil
				},
			},
			expectedReturnCode: 0,
		},
		"some config": {
			messages: []string{"\n"},

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
						Config: map[string]interface{}{"global": "true"},
					}, nil
				},
			},
			expectedReturnCode: 0,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := getInitializedCommand(t, buf)
			c.kubernetes = fake.NewSimpleClientset()
			c.helmActionsRunner = tc.helmActionsRunner
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
func getInitializedCommand(t *testing.T, buf io.Writer) *ReadCommand {
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

	c := &ReadCommand{
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
