// +build enterprise

package synccatalog

import (
	"fmt"
	"strings"
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

// Test syncing to a single destination consul namespace.
func TestRun_ToConsulSingleDestinationNamespace(t *testing.T) {
	t.Parallel()

	consulDestNamespaces := []string{"default", "destination"}
	for _, consulDestNamespace := range consulDestNamespaces {
		t.Run(consulDestNamespace, func(tt *testing.T) {
			k8s, testAgent := completeSetupEnterprise(tt)
			defer testAgent.Stop()

			// Run the command.
			ui := cli.NewMockUi()
			consulClient, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
			})
			require.NoError(tt, err)

			cmd := Command{
				UI:           ui,
				clientset:    k8s,
				consulClient: consulClient,
				logger: hclog.New(&hclog.LoggerOptions{
					Name:  tt.Name(),
					Level: hclog.Debug,
				}),
			}

			// Create two services in k8s in default and foo namespaces.
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

			exitChan := runCommandAsynchronously(&cmd, []string{
				"-consul-write-interval", "500ms",
				"-add-k8s-namespace-suffix",
				"-log-level=debug",
				"-enable-namespaces",
				"-consul-destination-namespace", consulDestNamespace,
				"-allow-k8s-namespace=*",
				"-add-k8s-namespace-suffix=false",
			})
			defer stopCommand(tt, &cmd, exitChan)

			timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
			retry.RunWith(timer, tt, func(r *retry.R) {
				// Both services should have been created in the destination namespace.
				for _, svcName := range []string{"default", "foo"} {
					instances, _, err := consulClient.Catalog().Service(svcName, "k8s", &api.QueryOptions{
						Namespace: consulDestNamespace,
					})
					require.NoError(r, err)
					require.Len(r, instances, 1)
					require.Equal(r, instances[0].ServiceName, svcName)
				}
			})
		})
	}
}

// Test syncing with mirroring and different prefixes.
func TestRun_ToConsulMirroringNamespaces(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		// MirroringPrefix is the value passed to -k8s-namespace-mirroring-prefix.
		MirroringPrefix string
		// ExtraFlags are extra flags for the command.
		ExtraFlags []string
		// ExpectNamespaceSuffix controls whether we expect the service names
		// to have their namespaces as a suffix.
		ExpectNamespaceSuffix bool
	}{
		"no prefix, no suffix": {
			MirroringPrefix:       "",
			ExtraFlags:            []string{"-add-k8s-namespace-suffix=false"},
			ExpectNamespaceSuffix: false,
		},
		"no prefix, with suffix": {
			MirroringPrefix:       "",
			ExtraFlags:            []string{"-add-k8s-namespace-suffix=true"},
			ExpectNamespaceSuffix: true,
		},
		"with prefix, no suffix": {
			MirroringPrefix:       "prefix-",
			ExtraFlags:            []string{"-add-k8s-namespace-suffix=false"},
			ExpectNamespaceSuffix: false,
		},
		"with prefix, with suffix": {
			MirroringPrefix:       "prefix-",
			ExtraFlags:            []string{"-add-k8s-namespace-suffix=true"},
			ExpectNamespaceSuffix: true,
		},
		"no prefix, no suffix, with destination namespace flag": {
			MirroringPrefix: "",
			// Mirroring takes precedence over the -consul-destination-namespace
			// flag so it should have no effect.
			ExtraFlags:            []string{"-add-k8s-namespace-suffix=false", "-consul-destination-namespace=dest"},
			ExpectNamespaceSuffix: false,
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			k8s, testAgent := completeSetupEnterprise(tt)
			defer testAgent.Stop()

			// Run the command.
			ui := cli.NewMockUi()
			consulClient, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
			})
			require.NoError(tt, err)

			cmd := Command{
				UI:           ui,
				clientset:    k8s,
				consulClient: consulClient,
				logger: hclog.New(&hclog.LoggerOptions{
					Name:  tt.Name(),
					Level: hclog.Debug,
				}),
			}

			// Create two services in k8s in default and foo namespaces.
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

			args := append([]string{
				"-consul-write-interval", "500ms",
				"-add-k8s-namespace-suffix",
				"-log-level=debug",
				"-enable-namespaces",
				"-allow-k8s-namespace=*",
				"-enable-k8s-namespace-mirroring",
				"-k8s-namespace-mirroring-prefix", c.MirroringPrefix,
			}, c.ExtraFlags...)
			exitChan := runCommandAsynchronously(&cmd, args)
			defer stopCommand(tt, &cmd, exitChan)

			timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
			retry.RunWith(timer, tt, func(r *retry.R) {
				// Each service should have been created in a mirrored namespace.
				for _, svcName := range []string{"default", "foo"} {
					// NOTE: svcName is the same as the kubernetes namespace.
					expNamespace := c.MirroringPrefix + svcName
					if c.ExpectNamespaceSuffix {
						// Since the service name is the same as the namespace,
						// in the case of the namespace suffix we expect
						// the service name to be suffixed.
						svcName = fmt.Sprintf("%s-%s", svcName, svcName)
					}
					instances, _, err := consulClient.Catalog().Service(svcName, "k8s", &api.QueryOptions{
						Namespace: expNamespace,
					})
					require.NoError(r, err)
					require.Len(r, instances, 1)
					require.Equal(r, instances[0].ServiceName, svcName)
				}
			})
		})
	}
}

// Test that when flags are changed and the command re-run, old services
// are deleted and new services are created where expected.
func TestRun_ToConsulChangingNamespaceFlags(t *testing.T) {
	t.Parallel()

	// There are many different settings:
	//   1. Namespaces enabled with a single destination namespace (single-dest-ns)
	//   2. Namespaces enabled with mirroring namespaces (mirroring-ns)
	//   3. Namespaces enabled with mirroring namespaces and prefixes (mirroring-ns-prefix)
	//
	// NOTE: In all cases, two services will be created in Kubernetes:
	//   1. namespace: default, name: default
	//   2. namespace: foo, name: foo

	cases := map[string]struct {
		// FirstRunFlags are the command flags for the first run of the command.
		FirstRunFlags []string
		// FirstRunExpServices are the services we expect to be created on the
		// first run. They're specified as "name/namespace".
		FirstRunExpServices []string
		// SecondRunFlags are the command flags for the second run of the command.
		SecondRunFlags []string
		// SecondRunExpServices are the services we expect to be created on the
		// second run. They're specified as "name/namespace".
		SecondRunExpServices []string
		// SecondRunExpDeletedServices are the services we expect to be deleted
		// on the second run. They're specified as "name/namespace".
		SecondRunExpDeletedServices []string
	}{
		"namespaces-disabled => single-dest-ns=default": {
			FirstRunFlags:       nil,
			FirstRunExpServices: []string{"foo/default", "default/default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=default",
			},
			SecondRunExpServices:        []string{"foo/default", "default/default"},
			SecondRunExpDeletedServices: nil,
		},
		"namespaces-disabled => single-dest-ns=dest": {
			FirstRunFlags:       nil,
			FirstRunExpServices: []string{"foo/default", "default/default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=dest",
			},
			SecondRunExpServices:        []string{"foo/dest", "default/dest"},
			SecondRunExpDeletedServices: []string{"foo/default", "default/default"},
		},
		"namespaces-disabled => mirroring-ns": {
			FirstRunFlags:       nil,
			FirstRunExpServices: []string{"foo/default", "default/default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
			},
			SecondRunExpServices:        []string{"foo/foo", "default/default"},
			SecondRunExpDeletedServices: []string{"foo/default"},
		},
		"namespaces-disabled => mirroring-ns-prefix": {
			FirstRunFlags:       nil,
			FirstRunExpServices: []string{"foo/default", "default/default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
				"-k8s-namespace-mirroring-prefix=prefix-",
			},
			SecondRunExpServices:        []string{"foo/prefix-foo", "default/prefix-default"},
			SecondRunExpDeletedServices: []string{"foo/default", "default/default"},
		},
		"single-dest-ns=first => single-dest-ns=second": {
			FirstRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=first",
			},
			FirstRunExpServices: []string{"foo/first", "default/first"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=second",
			},
			SecondRunExpServices:        []string{"foo/second", "default/second"},
			SecondRunExpDeletedServices: []string{"foo/first", "default/first"},
		},
		"single-dest-ns => mirroring-ns": {
			FirstRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=first",
			},
			FirstRunExpServices: []string{"foo/first", "default/first"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
			},
			SecondRunExpServices:        []string{"foo/foo", "default/default"},
			SecondRunExpDeletedServices: []string{"foo/first", "default/first"},
		},
		"single-dest-ns => mirroring-ns-prefix": {
			FirstRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=first",
			},
			FirstRunExpServices: []string{"foo/first", "default/first"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
				"-k8s-namespace-mirroring-prefix=prefix-",
			},
			SecondRunExpServices:        []string{"foo/prefix-foo", "default/prefix-default"},
			SecondRunExpDeletedServices: []string{"foo/first", "default/first"},
		},
		"mirroring-ns => single-dest-ns": {
			FirstRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
			},
			FirstRunExpServices: []string{"foo/foo", "default/default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=second",
			},
			SecondRunExpServices:        []string{"foo/second", "default/second"},
			SecondRunExpDeletedServices: []string{"foo/foo", "default/default"},
		},
		"mirroring-ns => mirroring-ns-prefix": {
			FirstRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
			},
			FirstRunExpServices: []string{"foo/foo", "default/default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
				"-k8s-namespace-mirroring-prefix=prefix-",
			},
			SecondRunExpServices:        []string{"foo/prefix-foo", "default/prefix-default"},
			SecondRunExpDeletedServices: []string{"foo/foo", "default/default"},
		},
		"mirroring-ns-prefix => single-dest-ns": {
			FirstRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
				"-k8s-namespace-mirroring-prefix=prefix-",
			},
			FirstRunExpServices: []string{"foo/prefix-foo", "default/prefix-default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-consul-destination-namespace=second",
			},
			SecondRunExpServices:        []string{"foo/second", "default/second"},
			SecondRunExpDeletedServices: []string{"foo/prefix-foo", "default/prefix-default"},
		},
		"mirroring-ns-prefix => mirroring-ns": {
			FirstRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
				"-k8s-namespace-mirroring-prefix=prefix-",
			},
			FirstRunExpServices: []string{"foo/prefix-foo", "default/prefix-default"},
			SecondRunFlags: []string{
				"-enable-namespaces",
				"-enable-k8s-namespace-mirroring",
			},
			SecondRunExpServices:        []string{"foo/foo", "default/default"},
			SecondRunExpDeletedServices: []string{"foo/prefix-foo", "default/prefix-default"},
		},
	}

	nameAndNS := func(s string) (string, string) {
		split := strings.Split(s, "/")
		return split[0], split[1]
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			k8s, testAgent := completeSetupEnterprise(tt)
			defer testAgent.Stop()
			ui := cli.NewMockUi()
			consulClient, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
			})
			require.NoError(tt, err)

			commonArgs := []string{
				"-consul-write-interval", "500ms",
				"-log-level=debug",
				"-allow-k8s-namespace=*",
			}

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
					for _, svcNamespace := range c.FirstRunExpServices {
						svcName, ns := nameAndNS(svcNamespace)
						instances, _, err := consulClient.Catalog().Service(svcName, "k8s", &api.QueryOptions{
							Namespace: ns,
						})
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
					for _, svcNamespace := range c.SecondRunExpServices {
						svcName, ns := nameAndNS(svcNamespace)
						instances, _, err := consulClient.Catalog().Service(svcName, "k8s", &api.QueryOptions{
							Namespace: ns,
						})
						require.NoError(r, err)
						require.Len(r, instances, 1)
						require.Equal(r, instances[0].ServiceName, svcName)
					}
				})
				tt.Log("existing services verified")

				timer = &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
				retry.RunWith(timer, tt, func(r *retry.R) {
					for _, svcNamespace := range c.SecondRunExpDeletedServices {
						svcName, ns := nameAndNS(svcNamespace)
						instances, _, err := consulClient.Catalog().Service(svcName, "k8s", &api.QueryOptions{
							Namespace: ns,
						})
						require.NoError(r, err)
						require.Len(r, instances, 0)
					}
				})
				tt.Log("deleted services verified")
			}
		})
	}
}

// Set up test consul agent and fake kubernetes cluster client
// todo: use this setup method everywhere. The old one (completeSetup) uses
// the test agent instead of the testserver.
func completeSetupEnterprise(t *testing.T) (*fake.Clientset, *testutil.TestServer) {
	k8s := fake.NewSimpleClientset()
	svr, err := testutil.NewTestServerT(t)
	require.NoError(t, err)
	return k8s, svr
}
