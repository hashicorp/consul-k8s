package synccatalog

import (
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Test that the default consul service is synced to k8s
func TestRun_Defaults_SyncsConsulServiceToK8s(t *testing.T) {
	t.Parallel()

	k8s, testServer := completeSetup(t)
	defer testServer.Stop()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
		logger: hclog.New(&hclog.LoggerOptions{
			Name:  t.Name(),
			Level: hclog.Debug,
		}),
	}

	exitChan := runCommandAsynchronously(&cmd, []string{
		"-http-addr", testServer.HTTPAddr,
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

	k8s, testServer := completeSetup(t)
	defer testServer.Stop()

	consulClient, err := api.NewClient(&api.Config{
		Address: testServer.HTTPAddr,
	})
	require.NoError(t, err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:           ui,
		clientset:    k8s,
		consulClient: consulClient,
		logger: hclog.New(&hclog.LoggerOptions{
			Name:  t.Name(),
			Level: hclog.Debug,
		}),
		flagAllowK8sNamespacesList: []string{"*"},
	}

	// create a service in k8s
	_, err = k8s.CoreV1().Services(metav1.NamespaceDefault).Create(lbService("foo", "1.1.1.1"))
	require.NoError(t, err)

	exitChan := runCommandAsynchronously(&cmd, []string{
		// change the write interval, so we can see changes in Consul quicker
		"-consul-write-interval", "500ms",
		"-add-k8s-namespace-suffix",
	})
	defer stopCommand(t, &cmd, exitChan)

	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		services, _, err := consulClient.Catalog().Services(nil)
		require.NoError(r, err)
		require.Len(r, services, 2)
		require.Contains(r, services, "foo-default")
	})
}

// Test that switching AddK8SNamespaceSuffix from false to true
// results in re-registering services in Consul with namespaced names
func TestCommand_Run_ToConsulChangeAddK8SNamespaceSuffixToTrue(t *testing.T) {
	t.Parallel()

	k8s, testServer := completeSetup(t)
	defer testServer.Stop()

	consulClient, err := api.NewClient(&api.Config{
		Address: testServer.HTTPAddr,
	})
	require.NoError(t, err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:           ui,
		clientset:    k8s,
		consulClient: consulClient,
		logger: hclog.New(&hclog.LoggerOptions{
			Name:  t.Name(),
			Level: hclog.Debug,
		}),
		flagAllowK8sNamespacesList: []string{"*"},
	}

	// create a service in k8s
	_, err = k8s.CoreV1().Services(metav1.NamespaceDefault).Create(lbService("foo", "1.1.1.1"))
	require.NoError(t, err)

	exitChan := runCommandAsynchronously(&cmd, []string{
		// change the write interval, so we can see changes in Consul quicker
		"-consul-write-interval", "1s",
	})

	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		services, _, err := consulClient.Catalog().Services(nil)
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
		services, _, err := consulClient.Catalog().Services(nil)
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

	k8s, testServer := completeSetup(t)
	defer testServer.Stop()

	consulClient, err := api.NewClient(&api.Config{
		Address: testServer.HTTPAddr,
	})
	require.NoError(t, err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:           ui,
		clientset:    k8s,
		consulClient: consulClient,
		logger: hclog.New(&hclog.LoggerOptions{
			Name:  t.Name(),
			Level: hclog.Debug,
		}),
		flagAllowK8sNamespacesList: []string{"*"},
	}

	// create two services in k8s
	_, err = k8s.CoreV1().Services("bar").Create(lbService("foo", "1.1.1.1"))
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
		svc, _, err := consulClient.Catalog().Service("foo-bar", "", nil)
		require.NoError(r, err)
		require.Len(r, svc, 1)
		require.Equal(r, "1.1.1.1", svc[0].ServiceAddress)
		svc, _, err = consulClient.Catalog().Service("foo-baz", "", nil)
		require.NoError(r, err)
		require.Len(r, svc, 1)
		require.Equal(r, "2.2.2.2", svc[0].ServiceAddress)
	})
}

// Test the allow/deny list combinations.
func TestRun_ToConsulAllowDenyLists(t *testing.T) {
	t.Parallel()

	// NOTE: In all cases, two services will be created in Kubernetes:
	//   1. namespace: default, name: default
	//   2. namespace: foo, name: foo

	cases := map[string]struct {
		AllowList   []string
		DenyList    []string
		ExpServices []string
	}{
		"empty lists": {
			AllowList:   nil,
			DenyList:    nil,
			ExpServices: nil,
		},
		"only from allow list": {
			AllowList:   []string{"foo"},
			DenyList:    nil,
			ExpServices: []string{"foo"},
		},
		"both in allow and deny": {
			AllowList:   []string{"foo"},
			DenyList:    []string{"foo"},
			ExpServices: nil,
		},
		"deny removes one from allow": {
			AllowList:   []string{"foo", "default"},
			DenyList:    []string{"foo"},
			ExpServices: []string{"default"},
		},
		"* in allow": {
			AllowList:   []string{"*"},
			DenyList:    nil,
			ExpServices: []string{"foo", "default"},
		},
		"* in allow with one denied": {
			AllowList:   []string{"*"},
			DenyList:    []string{"foo"},
			ExpServices: []string{"default"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			k8s, testServer := completeSetup(tt)
			defer testServer.Stop()

			consulClient, err := api.NewClient(&api.Config{
				Address: testServer.HTTPAddr,
			})
			require.NoError(t, err)

			// Create two services in k8s in default and foo namespaces.
			{
				_, err = k8s.CoreV1().Services(metav1.NamespaceDefault).Create(lbService("default", "1.1.1.1"))
				require.NoError(tt, err)
				_, err = k8s.CoreV1().Namespaces().Create(&apiv1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
				})
				require.NoError(tt, err)
				_, err = k8s.CoreV1().Services("foo").Create(lbService("foo", "1.1.1.1"))
				require.NoError(tt, err)
			}

			flags := []string{
				"-consul-write-interval", "500ms",
				"-log-level=debug",
			}
			for _, allow := range c.AllowList {
				flags = append(flags, "-allow-k8s-namespace", allow)
			}
			for _, deny := range c.DenyList {
				flags = append(flags, "-deny-k8s-namespace", deny)
			}

			// Run the command
			ui := cli.NewMockUi()
			cmd := Command{
				UI:           ui,
				clientset:    k8s,
				consulClient: consulClient,
				logger: hclog.New(&hclog.LoggerOptions{
					Name:  tt.Name(),
					Level: hclog.Debug,
				}),
			}
			exitChan := runCommandAsynchronously(&cmd, flags)
			defer stopCommand(tt, &cmd, exitChan)

			timer := &retry.Timer{Timeout: 2 * time.Second, Wait: 500 * time.Millisecond}
			retry.RunWith(timer, tt, func(r *retry.R) {
				svcs, _, err := consulClient.Catalog().Services(nil)
				require.NoError(r, err)
				// There should be the number of expected services plus one
				// for the default Consul service.
				require.Len(r, svcs, len(c.ExpServices)+1)
				for _, svc := range c.ExpServices {
					instances, _, err := consulClient.Catalog().Service(svc, "k8s", nil)
					require.NoError(r, err)
					require.Len(r, instances, 1)
					require.Equal(r, instances[0].ServiceName, svc)
				}
			})
		})
	}
}

// Test that when flags are changed and the command re-run, old services
// are deleted and new services are created where expected.
func TestRun_ToConsulChangingFlags(t *testing.T) {
	t.Parallel()

	// NOTE: In all cases, two services will be created in Kubernetes:
	//   1. namespace: default, name: default
	//   2. namespace: foo, name: foo
	//
	// NOTE: We're not testing all permutations the allow/deny lists. That is
	// tested in TestRun_ToConsulAllowDenyLists. We assume that that test
	// ensures the allow/deny lists are working and so all we need to test here
	// is that if the resulting set of namespaces changes, we add/remove services
	// accordingly.

	cases := map[string]struct {
		// FirstRunFlags are the command flags for the first run of the command.
		FirstRunFlags []string
		// FirstRunExpServices are the services we expect to be created on the
		// first run.
		FirstRunExpServices []string
		// SecondRunFlags are the command flags for the second run of the command.
		SecondRunFlags []string
		// SecondRunExpServices are the services we expect to be created on the
		// second run.
		SecondRunExpServices []string
		// SecondRunExpDeletedServices are the services we expect to be deleted
		// on the second run.
		SecondRunExpDeletedServices []string
	}{
		"service-suffix-false => service-suffix-true": {
			FirstRunFlags: []string{
				"-allow-k8s-namespace=*",
				"-add-k8s-namespace-suffix=false",
			},
			FirstRunExpServices: []string{"foo", "default"},
			SecondRunFlags: []string{
				"-allow-k8s-namespace=*",
				"-add-k8s-namespace-suffix=true",
			},
			SecondRunExpServices:        []string{"foo-foo", "default-default"},
			SecondRunExpDeletedServices: []string{"foo", "default"},
		},
		"service-suffix-true => service-suffix-false": {
			FirstRunFlags: []string{
				"-allow-k8s-namespace=*",
				"-add-k8s-namespace-suffix=true",
			},
			FirstRunExpServices: []string{"foo-foo", "default-default"},
			SecondRunFlags: []string{
				"-allow-k8s-namespace=*",
				"-add-k8s-namespace-suffix=false",
			},
			SecondRunExpServices:        []string{"foo", "default"},
			SecondRunExpDeletedServices: []string{"foo-default", "default-default"},
		},
		"allow-k8s-namespace=* => allow-k8s-namespace=default": {
			FirstRunFlags: []string{
				"-allow-k8s-namespace=*",
			},
			FirstRunExpServices: []string{"foo", "default"},
			SecondRunFlags: []string{
				"-allow-k8s-namespace=default",
			},
			SecondRunExpServices:        []string{"default"},
			SecondRunExpDeletedServices: []string{"foo"},
		},
		"allow-k8s-namespace=default => allow-k8s-namespace=*": {
			FirstRunFlags: []string{
				"-allow-k8s-namespace=default",
			},
			FirstRunExpServices: []string{"default"},
			SecondRunFlags: []string{
				"-allow-k8s-namespace=*",
			},
			SecondRunExpServices:        []string{"foo", "default"},
			SecondRunExpDeletedServices: nil,
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			k8s, testServer := completeSetup(tt)
			defer testServer.Stop()

			consulClient, err := api.NewClient(&api.Config{
				Address: testServer.HTTPAddr,
			})
			require.NoError(t, err)

			ui := cli.NewMockUi()

			commonArgs := []string{
				"-consul-write-interval", "500ms",
				"-log-level=debug",
			}

			// Create two services in k8s in default and foo namespaces.
			{
				_, err := k8s.CoreV1().Services(metav1.NamespaceDefault).Create(lbService("default", "1.1.1.1"))
				require.NoError(tt, err)
				_, err = k8s.CoreV1().Namespaces().Create(&apiv1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
				})
				require.NoError(tt, err)
				_, err = k8s.CoreV1().Services("foo").Create(lbService("foo", "1.1.1.1"))
				require.NoError(tt, err)
			}

			// Run the first command.
			{
				firstCmd := Command{
					UI:           ui,
					clientset:    k8s,
					consulClient: consulClient,
					logger: hclog.New(&hclog.LoggerOptions{
						Name:  tt.Name() + "-firstrun",
						Level: hclog.Debug,
					}),
				}
				exitChan := runCommandAsynchronously(&firstCmd, append(commonArgs, c.FirstRunFlags...))

				// Wait until the expected services are synced.
				timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
				retry.RunWith(timer, tt, func(r *retry.R) {
					for _, svcName := range c.FirstRunExpServices {
						instances, _, err := consulClient.Catalog().Service(svcName, "k8s", nil)
						require.NoError(r, err)
						require.Len(r, instances, 1)
						require.Equal(r, instances[0].ServiceName, svcName)
					}
				})
				stopCommand(tt, &firstCmd, exitChan)
			}
			tt.Log("first command run complete")

			// Run the second command.
			{
				secondCmd := Command{
					UI:           ui,
					clientset:    k8s,
					consulClient: consulClient,
					logger: hclog.New(&hclog.LoggerOptions{
						Name:  tt.Name() + "-secondrun",
						Level: hclog.Debug,
					}),
				}
				exitChan := runCommandAsynchronously(&secondCmd, append(commonArgs, c.SecondRunFlags...))
				defer stopCommand(tt, &secondCmd, exitChan)

				// Wait until the expected services are synced and the old ones
				// deleted.
				timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
				retry.RunWith(timer, tt, func(r *retry.R) {
					for _, svcName := range c.SecondRunExpServices {
						instances, _, err := consulClient.Catalog().Service(svcName, "k8s", nil)
						require.NoError(r, err)
						require.Len(r, instances, 1)
						require.Equal(r, instances[0].ServiceName, svcName)
					}
					tt.Log("existing services verified")

					for _, svcName := range c.SecondRunExpDeletedServices {
						instances, _, err := consulClient.Catalog().Service(svcName, "k8s", nil)
						require.NoError(r, err)
						require.Len(r, instances, 0)
					}
					tt.Log("deleted services verified")
				})
			}
		})
	}
}

// Set up test consul agent and fake kubernetes cluster client
func completeSetup(t *testing.T) (*fake.Clientset, *testutil.TestServer) {
	k8s := fake.NewSimpleClientset()

	svr, err := testutil.NewTestServerT(t)
	require.NoError(t, err)

	return k8s, svr
}

// This function starts the command asynchronously and returns a non-blocking chan.
// When finished, the command will send its exit code to the channel.
// Note that it's the responsibility of the caller to terminate the command by calling stopCommand,
// otherwise it can run forever.
func runCommandAsynchronously(cmd *Command, args []string) chan int {
	// We have to run cmd.init() to ensure that the channel the command is
	// using to watch for os interrupts is initialized. If we don't do this,
	// then if stopCommand is called immediately, it will block forever
	// because it calls interrupt() which will attempt to send on a nil channel.
	cmd.init()
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
