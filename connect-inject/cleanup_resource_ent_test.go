// +build enterprise

package connectinject

import (
	"net/url"
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReconcile_ConsulNamespaces(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		ConsulServices []capi.AgentServiceRegistration
		KubePods       []runtime.Object
		// ExpConsulServiceIDs maps from Consul namespace to
		// list of expected service ids in that namespace.
		ExpConsulServiceIDs map[string][]string
	}{
		"default namespace, pod deleted": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcDefaultNS,
			},
			KubePods: nil,
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul"},
			},
		},
		"default namespace, pod not deleted": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcDefaultNS,
			},
			KubePods: []runtime.Object{consulFooPodDefaultNS},
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul", "foo-abc123-foo"},
			},
		},
		"foo namespace, pod deleted": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcFooNS,
			},
			KubePods: nil,
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul"},
				"foo":     nil,
			},
		},
		"foo namespace, pod not deleted": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcFooNS,
			},
			KubePods: []runtime.Object{consulFooPodFooNS},
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul"},
				"foo":     {"foo-abc123-foo"},
			},
		},
		"does not delete instances with same id in different namespaces": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcFooNS,
				consulFooSvcBarNS,
			},
			KubePods: []runtime.Object{consulFooPodFooNS},
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul"},
				"foo":     {"foo-abc123-foo"},
				"bar":     nil,
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// Start Consul server.
			server, err := testutil.NewTestServerConfigT(t, nil)
			defer server.Stop()
			require.NoError(err)
			server.WaitForLeader(t)
			consulClient, err := capi.NewClient(&capi.Config{Address: server.HTTPAddr})
			require.NoError(err)

			// Register Consul services.
			for _, svc := range c.ConsulServices {
				_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
					Name: svc.Namespace,
				}, nil)
				require.NoError(err)
				require.NoError(consulClient.Agent().ServiceRegister(&svc))
			}

			// Create the cleanup resource.
			log := hclog.Default().Named("cleanupResource")
			log.SetLevel(hclog.Debug)
			consulURL, err := url.Parse("http://" + server.HTTPAddr)
			require.NoError(err)
			node := nodeName(t, consulClient)
			// NOTE: we need to add the node because the reconciler checks if
			// the node the service is registered with actually exists in this
			// cluster.
			kubeResources := append(c.KubePods, &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: node,
				},
			})
			cleanupResource := CleanupResource{
				Log:                    log,
				KubernetesClient:       fake.NewSimpleClientset(kubeResources...),
				ConsulClient:           consulClient,
				ConsulScheme:           consulURL.Scheme,
				ConsulPort:             consulURL.Port(),
				EnableConsulNamespaces: true,
			}

			// Run Reconcile.
			cleanupResource.reconcile()

			// Test that the remaining services are what we expect.
			for ns, expSvcs := range c.ExpConsulServiceIDs {
				// Note: we need to use the catalog endpoints because
				// Agent().Services() does not currently support namespaces
				// (https://github.com/hashicorp/consul/issues/9710).
				services, _, err := consulClient.Catalog().Services(&capi.QueryOptions{Namespace: ns})
				require.NoError(err)

				var actualServiceIDs []string
				for actSvcName := range services {
					services, _, err := consulClient.Catalog().Service(actSvcName, "", &capi.QueryOptions{Namespace: ns})
					require.NoError(err)
					for _, actSvc := range services {
						actualServiceIDs = append(actualServiceIDs, actSvc.ServiceID)
					}
				}
				require.ElementsMatch(actualServiceIDs, expSvcs, "ns=%s act=%v", ns, actualServiceIDs)
			}
		})
	}
}

func TestDelete_ConsulNamespaces(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		Pod            *corev1.Pod
		ConsulServices []capi.AgentServiceRegistration
		// ExpConsulServiceIDs maps from Consul namespace to
		// list of expected service ids in that namespace.
		ExpConsulServiceIDs map[string][]string
		ExpErr              string
	}{
		"default namespace": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcDefaultNS,
			},
			Pod: consulFooPodDefaultNS,
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul"},
			},
		},
		"foo namespace": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcFooNS,
			},
			Pod: consulFooPodFooNS,
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul"},
				"foo":     nil,
			},
		},
		"does not delete instances with same id in different namespaces": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvcFooNS,
				consulFooSvcBarNS,
			},
			Pod: consulFooPodFooNS,
			ExpConsulServiceIDs: map[string][]string{
				"default": {"consul"},
				"foo":     nil,
				"bar":     {"foo-abc123-foo"},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// Start Consul server.
			server, err := testutil.NewTestServerConfigT(t, nil)
			defer server.Stop()
			require.NoError(err)
			server.WaitForLeader(t)
			consulClient, err := capi.NewClient(&capi.Config{Address: server.HTTPAddr})
			require.NoError(err)

			// Register Consul services.
			for _, svc := range c.ConsulServices {
				_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
					Name: svc.Namespace,
				}, nil)
				require.NoError(err)
				require.NoError(consulClient.Agent().ServiceRegister(&svc))
			}

			// Create the cleanup resource.
			log := hclog.Default().Named("cleanupResource")
			log.SetLevel(hclog.Debug)
			consulURL, err := url.Parse("http://" + server.HTTPAddr)
			require.NoError(err)
			cleanupResource := CleanupResource{
				Log:                    log,
				KubernetesClient:       fake.NewSimpleClientset(),
				ConsulClient:           consulClient,
				ConsulScheme:           consulURL.Scheme,
				ConsulPort:             consulURL.Port(),
				EnableConsulNamespaces: true,
			}

			// Run Delete.
			err = cleanupResource.Delete("default/foo", c.Pod)
			if c.ExpErr != "" {
				require.EqualError(err, c.ExpErr)
			} else {
				require.NoError(err)

				// Test that the remaining services are what we expect.
				for ns, expSvcs := range c.ExpConsulServiceIDs {
					// Note: we need to use the catalog endpoints because
					// Agent().Services() does not currently support namespaces
					// (https://github.com/hashicorp/consul/issues/9710).
					services, _, err := consulClient.Catalog().Services(&capi.QueryOptions{Namespace: ns})
					require.NoError(err)

					var actualServiceIDs []string
					for actSvcName := range services {
						services, _, err := consulClient.Catalog().Service(actSvcName, "", &capi.QueryOptions{Namespace: ns})
						require.NoError(err)
						for _, actSvc := range services {
							actualServiceIDs = append(actualServiceIDs, actSvc.ServiceID)
						}
					}
					require.ElementsMatch(actualServiceIDs, expSvcs, "ns=%s act=%v", ns, actualServiceIDs)
				}
			}
		})
	}
}

var (
	consulFooSvcDefaultNS = capi.AgentServiceRegistration{
		ID:        "foo-abc123-foo",
		Name:      "foo",
		Namespace: "default",
		Address:   "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "foo-abc123",
			MetaKeyKubeNS:  "default",
		},
	}
	consulFooSvcFooNS = capi.AgentServiceRegistration{
		ID:        "foo-abc123-foo",
		Name:      "foo",
		Namespace: "foo",
		Address:   "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "foo-abc123",
			MetaKeyKubeNS:  "default",
		},
	}
	consulFooSvcBarNS = capi.AgentServiceRegistration{
		ID:        "foo-abc123-foo",
		Name:      "foo",
		Namespace: "bar",
		Address:   "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "foo-abc123",
			MetaKeyKubeNS:  "bar",
		},
	}
	consulFooPodDefaultNS = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo-abc123",
			Namespace: "default",
			Labels: map[string]string{
				labelInject: injected,
			},
			Annotations: map[string]string{
				annotationStatus:          injected,
				annotationService:         "foo",
				annotationConsulNamespace: "default",
			},
		},
		Status: corev1.PodStatus{
			HostIP: "127.0.0.1",
		},
	}
	consulFooPodFooNS = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo-abc123",
			Namespace: "default",
			Labels: map[string]string{
				labelInject: injected,
			},
			Annotations: map[string]string{
				annotationStatus:          injected,
				annotationService:         "foo",
				annotationConsulNamespace: "foo",
			},
		},
		Status: corev1.PodStatus{
			HostIP: "127.0.0.1",
		},
	}
)
