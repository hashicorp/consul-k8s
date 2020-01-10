package synccatalog

import (
	"testing"
	"time"

	"github.com/hashicorp/consul/agent"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Test that the default consul service is synced to k8s
func TestRun_Defaults_SyncsConsulServiceToK8s(t *testing.T) {
	t.Parallel()

	k8s, testAgent := completeSetup(t)
	defer testAgent.Shutdown()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	exitChan := runCommandAsynchronously(&cmd, []string{
		"-http-addr", testAgent.HTTPAddr(),
	})
	defer stopCommand(t, &cmd, exitChan)

	retry.Run(t, func(r *retry.R) {
		serviceList, err := k8s.CoreV1().Services(metav1.NamespaceDefault).List(metav1.ListOptions{})
		require.NoError(r, err)
		require.Len(r, serviceList.Items, 1)
		require.Equal(r, "consul", serviceList.Items[0].Name)
		require.Equal(r, "consul.service.consul", serviceList.Items[0].Spec.ExternalName)
	})
}

// Test that when -add-k8s-namespace-suffix flag is used
// k8s namespaces are appended to the service names synced to Consul
func TestRun_ToConsulWithAddK8SNamespaceSuffix(t *testing.T) {
	t.Parallel()

	k8s, testAgent := completeSetup(t)
	defer testAgent.Shutdown()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:                         ui,
		clientset:                  k8s,
		consulClient:               testAgent.Client(),
		flagAllowK8sNamespacesList: []string{"*"},
	}

	// create a service in k8s
	_, err := k8s.CoreV1().Services(metav1.NamespaceDefault).Create(lbService("foo", "1.1.1.1"))
	require.NoError(t, err)

	exitChan := runCommandAsynchronously(&cmd, []string{
		// change the write interval, so we can see changes in Consul quicker
		"-consul-write-interval", "500ms",
		"-add-k8s-namespace-suffix",
	})
	defer stopCommand(t, &cmd, exitChan)

	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		services, _, err := testAgent.Client().Catalog().Services(nil)
		require.NoError(r, err)
		require.Len(r, services, 2)
		require.Contains(r, services, "foo-default")
	})
}

// Test that switching AddK8SNamespaceSuffix from false to true
// results in re-registering services in Consul with namespaced names
func TestCommand_Run_ToConsulChangeAddK8SNamespaceSuffixToTrue(t *testing.T) {
	t.Parallel()

	k8s, testAgent := completeSetup(t)
	defer testAgent.Shutdown()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:                         ui,
		clientset:                  k8s,
		consulClient:               testAgent.Client(),
		flagAllowK8sNamespacesList: []string{"*"},
	}

	// create a service in k8s
	_, err := k8s.CoreV1().Services(metav1.NamespaceDefault).Create(lbService("foo", "1.1.1.1"))
	require.NoError(t, err)

	exitChan := runCommandAsynchronously(&cmd, []string{
		// change the write interval, so we can see changes in Consul quicker
		"-consul-write-interval", "1s",
	})

	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		services, _, err := testAgent.Client().Catalog().Services(nil)
		require.NoError(r, err)
		require.Len(r, services, 2)
		require.Contains(r, services, "foo")
	})

	stopCommand(t, &cmd, exitChan)

	// restart sync with -add-k8s-namespace-suffix
	exitChan = runCommandAsynchronously(&cmd, []string{
		"-consul-write-interval", "1s",
		"-add-k8s-namespace-suffix",
	})
	defer stopCommand(t, &cmd, exitChan)

	// check that the name of the service is now namespaced
	retry.RunWith(timer, t, func(r *retry.R) {
		services, _, err := testAgent.Client().Catalog().Services(nil)
		require.NoError(r, err)
		require.Len(r, services, 2)
		require.Contains(r, services, "foo-default")
	})
}

// Test that services with same name but in different namespaces
// get registered as different services in consul
// when using -add-k8s-namespace-suffix
func TestCommand_Run_ToConsulTwoServicesSameNameDifferentNamespace(t *testing.T) {
	t.Parallel()

	k8s, testAgent := completeSetup(t)
	defer testAgent.Shutdown()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:                         ui,
		clientset:                  k8s,
		consulClient:               testAgent.Client(),
		flagAllowK8sNamespacesList: []string{"*"},
	}

	// create two services in k8s
	_, err := k8s.CoreV1().Services("bar").Create(lbService("foo", "1.1.1.1"))
	require.NoError(t, err)

	_, err = k8s.CoreV1().Services("baz").Create(lbService("foo", "2.2.2.2"))
	require.NoError(t, err)

	exitChan := runCommandAsynchronously(&cmd, []string{
		"-consul-write-interval", "1s",
		"-add-k8s-namespace-suffix",
	})
	defer stopCommand(t, &cmd, exitChan)

	// check that the name of the service is namespaced
	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		svc, _, err := testAgent.Client().Catalog().Service("foo-bar", "", nil)
		require.NoError(r, err)
		require.Len(r, svc, 1)
		require.Equal(r, "1.1.1.1", svc[0].ServiceAddress)
		svc, _, err = testAgent.Client().Catalog().Service("foo-baz", "", nil)
		require.NoError(r, err)
		require.Len(r, svc, 1)
		require.Equal(r, "2.2.2.2", svc[0].ServiceAddress)
	})
}

// Set up test consul agent and fake kubernetes cluster client
func completeSetup(t *testing.T) (*fake.Clientset, *agent.TestAgent) {
	k8s := fake.NewSimpleClientset()
	a := agent.NewTestAgent(t, t.Name(), `primary_datacenter = "dc1"`)

	return k8s, a
}

// This function starts the command asynchronously and returns a non-blocking chan.
// When finished, the command will send its exit code to the channel.
// Note that it's the responsibility of the caller to terminate the command by calling stopCommand,
// otherwise it can run forever.
func runCommandAsynchronously(cmd *Command, args []string) chan int {
	exitChan := make(chan int, 1)

	go func() {
		exitChan <- cmd.Run(args)
	}()

	return exitChan
}

func stopCommand(t *testing.T, cmd *Command, exitChan chan int) {
	if len(exitChan) == 0 {
		cmd.interrupt()
	}
	select {
	case c := <-exitChan:
		require.Equal(t, 0, c, string(cmd.UI.(*cli.MockUi).ErrorWriter.Bytes()))
	}
}

// lbService returns a Kubernetes service of type LoadBalancer.
func lbService(name, lbIP string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeLoadBalancer,
		},

		Status: apiv1.ServiceStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					{
						IP: lbIP,
					},
				},
			},
		},
	}
}
