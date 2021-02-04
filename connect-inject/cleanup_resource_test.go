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

func TestReconcile(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		ConsulServices      []capi.AgentServiceRegistration
		KubePods            []runtime.Object
		ExpConsulServiceIDs []string
		// OutOfClusterNode controls whether the services are registered on a
		// node that does not exist in this Kube cluster.
		OutOfClusterNode bool
	}{
		"no instances running": {
			ConsulServices:      nil,
			KubePods:            nil,
			ExpConsulServiceIDs: nil,
		},
		"instance does not have pod-name meta key": {
			ConsulServices:      []capi.AgentServiceRegistration{consulNoPodNameMetaSvc},
			ExpConsulServiceIDs: []string{"no-pod-name-meta"},
		},
		"instance does not have k8s-namespace meta key": {
			ConsulServices:      []capi.AgentServiceRegistration{consulNoK8sNSMetaSvc},
			ExpConsulServiceIDs: []string{"no-k8s-ns-meta"},
		},
		"out of cluster node": {
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvc, consulFooSvcSidecar},
			ExpConsulServiceIDs: []string{"foo-abc123-foo", "foo-abc123-foo-sidecar-proxy"},
			OutOfClusterNode:    true,
		},
		"app and sidecar still running": {
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvc, consulFooSvcSidecar},
			KubePods:            []runtime.Object{fooPod},
			ExpConsulServiceIDs: []string{"foo-abc123-foo", "foo-abc123-foo-sidecar-proxy"},
		},
		"app and sidecar terminated": {
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvc, consulFooSvcSidecar},
			KubePods:            nil,
			ExpConsulServiceIDs: nil,
		},
		"only app is registered, no sidecar": {
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvc},
			KubePods:            nil,
			ExpConsulServiceIDs: nil,
		},
		"only sidecar is registered, no app": {
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvcSidecar},
			KubePods:            nil,
			ExpConsulServiceIDs: nil,
		},
		"multiple instances of the same service": {
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvc,
				consulFooSvcSidecar,
				consulFooSvcPod2,
				consulFooSvcSidecarPod2,
			},
			KubePods:            []runtime.Object{fooPod},
			ExpConsulServiceIDs: []string{"foo-abc123-foo", "foo-abc123-foo-sidecar-proxy"},
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
				require.NoError(consulClient.Agent().ServiceRegister(&svc))
			}

			// Create the cleanup resource.
			log := hclog.Default().Named("cleanupResource")
			log.SetLevel(hclog.Debug)
			consulURL, err := url.Parse("http://" + server.HTTPAddr)
			require.NoError(err)
			kubeResources := c.KubePods
			if !c.OutOfClusterNode {
				node := nodeName(t, consulClient)
				// NOTE: we need to add the node because the reconciler checks if
				// the node the service is registered with actually exists in this
				// cluster.
				kubeResources = append(kubeResources, &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: node,
					},
				})

			}
			cleanupResource := CleanupResource{
				Log:              log,
				KubernetesClient: fake.NewSimpleClientset(kubeResources...),
				ConsulClient:     consulClient,
				ConsulScheme:     consulURL.Scheme,
				ConsulPort:       consulURL.Port(),
			}

			// Run Reconcile.
			cleanupResource.reconcile()

			// Test that the remaining services are what we expect.
			services, err := consulClient.Agent().Services()
			require.NoError(err)
			var actualServiceIDs []string
			for id := range services {
				actualServiceIDs = append(actualServiceIDs, id)
			}
			require.ElementsMatch(actualServiceIDs, c.ExpConsulServiceIDs)
		})
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		Pod                 *corev1.Pod
		ConsulServices      []capi.AgentServiceRegistration
		ExpConsulServiceIDs []string
		ExpErr              string
	}{
		"pod is nil": {
			Pod:    nil,
			ExpErr: "object for key default/foo was nil",
		},
		"pod does not have service-name annotation": {
			Pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo-abc123",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					HostIP: "127.0.0.1",
				},
			},
			ExpErr: "pod did not have consul.hashicorp.com/connect-service annotation",
		},
		"no instances still registered": {
			Pod:                 fooPod,
			ConsulServices:      nil,
			ExpConsulServiceIDs: nil,
		},
		"app and sidecar terminated": {
			Pod:                 fooPod,
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvc, consulFooSvcSidecar},
			ExpConsulServiceIDs: nil,
		},
		"only app is registered, no sidecar": {
			Pod:                 fooPod,
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvc},
			ExpConsulServiceIDs: nil,
		},
		"only sidecar is registered, no app": {
			Pod:                 fooPod,
			ConsulServices:      []capi.AgentServiceRegistration{consulFooSvcSidecar},
			ExpConsulServiceIDs: nil,
		},
		"multiple instances of the same service": {
			Pod: fooPod,
			ConsulServices: []capi.AgentServiceRegistration{
				consulFooSvc,
				consulFooSvcSidecar,
				consulFooSvcPod2,
				consulFooSvcSidecarPod2,
			},
			ExpConsulServiceIDs: []string{"foo-def456-foo", "foo-def456-foo-sidecar-proxy"},
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
				require.NoError(consulClient.Agent().ServiceRegister(&svc))
			}

			// Create the cleanup resource.
			log := hclog.Default().Named("cleanupResource")
			log.SetLevel(hclog.Debug)
			consulURL, err := url.Parse("http://" + server.HTTPAddr)
			require.NoError(err)
			cleanupResource := CleanupResource{
				Log:              log,
				KubernetesClient: fake.NewSimpleClientset(),
				ConsulClient:     consulClient,
				ConsulScheme:     consulURL.Scheme,
				ConsulPort:       consulURL.Port(),
			}

			// Run Delete.
			err = cleanupResource.Delete("default/foo", c.Pod)
			if c.ExpErr != "" {
				require.EqualError(err, c.ExpErr)
			} else {
				require.NoError(err)

				// Test that the remaining services are what we expect.
				services, err := consulClient.Agent().Services()
				require.NoError(err)
				var actualServiceIDs []string
				for id := range services {
					actualServiceIDs = append(actualServiceIDs, id)
				}
				require.ElementsMatch(actualServiceIDs, c.ExpConsulServiceIDs)
			}
		})
	}
}

// nodeName returns the Consul node name for the agent that client
// points at.
func nodeName(t *testing.T, client *capi.Client) string {
	self, err := client.Agent().Self()
	require.NoError(t, err)
	require.Contains(t, self, "Config")
	require.Contains(t, self["Config"], "NodeName")
	return self["Config"]["NodeName"].(string)
}

var (
	consulFooSvc = capi.AgentServiceRegistration{
		ID:      "foo-abc123-foo",
		Name:    "foo",
		Address: "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "foo-abc123",
			MetaKeyKubeNS:  "default",
		},
	}
	consulFooSvcSidecar = capi.AgentServiceRegistration{
		ID:      "foo-abc123-foo-sidecar-proxy",
		Name:    "foo-sidecar-proxy",
		Address: "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "foo-abc123",
			MetaKeyKubeNS:  "default",
		},
	}
	consulFooSvcPod2 = capi.AgentServiceRegistration{
		ID:      "foo-def456-foo",
		Name:    "foo",
		Address: "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "foo-def456",
			MetaKeyKubeNS:  "default",
		},
	}
	consulFooSvcSidecarPod2 = capi.AgentServiceRegistration{
		ID:      "foo-def456-foo-sidecar-proxy",
		Name:    "foo-sidecar-proxy",
		Address: "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "foo-def456",
			MetaKeyKubeNS:  "default",
		},
	}
	consulNoPodNameMetaSvc = capi.AgentServiceRegistration{
		ID:      "no-pod-name-meta",
		Name:    "no-pod-name-meta",
		Address: "127.0.0.1",
		Meta: map[string]string{
			MetaKeyKubeNS: "default",
		},
	}
	consulNoK8sNSMetaSvc = capi.AgentServiceRegistration{
		ID:      "no-k8s-ns-meta",
		Name:    "no-k8s-ns-meta",
		Address: "127.0.0.1",
		Meta: map[string]string{
			MetaKeyPodName: "no-k8s-ns-meta",
		},
	}
	fooPod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo-abc123",
			Namespace: "default",
			Labels: map[string]string{
				labelInject: injected,
			},
			Annotations: map[string]string{
				annotationStatus:  injected,
				annotationService: "foo",
			},
		},
		Status: corev1.PodStatus{
			HostIP: "127.0.0.1",
		},
	}
)
