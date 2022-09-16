package status

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestCheckConsulServers creates a fake stateful set and tests the checkConsulServers function.
func TestCheckConsulServers(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()

	// First check that no stateful sets causes an error.
	_, err := c.checkConsulServers("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no server stateful set found")

	// Next create a stateful set with 3 desired replicas and 3 ready replicas.
	var replicas int32 = 3

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-server-test1",
			Namespace: "default",
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas,
		},
	}

	c.kubernetes.AppsV1().StatefulSets("default").Create(context.Background(), ss, metav1.CreateOptions{})

	// Now we run the checkConsulServers() function and it should succeed.
	s, err := c.checkConsulServers("default")
	require.NoError(t, err)
	require.Equal(t, "Consul servers healthy (3/3)", s)

	// If you then create another stateful set it should error.
	ss2 := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-server-test2",
			Namespace: "default",
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas,
		},
	}
	c.kubernetes.AppsV1().StatefulSets("default").Create(context.Background(), ss2, metav1.CreateOptions{})

	_, err = c.checkConsulServers("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "found multiple server stateful sets")

	// Clear out the client and now run a test where the stateful set isn't ready.
	c.kubernetes = fake.NewSimpleClientset()

	ss3 := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-server-test3",
			Namespace: "default",
			Labels:    map[string]string{"app": "consul", "chart": "consul-helm", "component": "server"},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas - 1, // Let's just set one of the servers to unhealthy
		},
	}
	c.kubernetes.AppsV1().StatefulSets("default").Create(context.Background(), ss3, metav1.CreateOptions{})

	_, err = c.checkConsulServers("default")
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("%d/%d Consul servers unhealthy", 1, replicas))
}

// TestCheckConsulClients is very similar to TestCheckConsulServers() in structure.
func TestCheckConsulClients(t *testing.T) {
	c := getInitializedCommand(t)
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

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})

	baseCommand := &common.BaseCommand{
		Log: log,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	cmd := getInitializedCommand(t)

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
	cmd := getInitializedCommand(t)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}
