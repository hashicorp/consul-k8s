package connectinject

import (
	"fmt"
	"strings"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestShouldIgnore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		namespace string
		denySet   mapset.Set
		allowSet  mapset.Set
		expected  bool
	}{
		{
			name:      "system namespace",
			namespace: "kube-system",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("*"),
			expected:  true,
		},
		{
			name:      "other system namespace",
			namespace: "local-path-storage",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("*"),
			expected:  true,
		},
		{
			name:      "any namespace allowed",
			namespace: "foo",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("*"),
			expected:  false,
		},
		{
			name:      "in deny list",
			namespace: "foo",
			denySet:   mapset.NewSetWith("foo"),
			allowSet:  mapset.NewSetWith("*"),
			expected:  true,
		},
		{
			name:      "not in allow list",
			namespace: "foo",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("bar"),
			expected:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			actual := shouldIgnore(tt.namespace, tt.denySet, tt.allowSet)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestHasBeenInjected(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		pod      func() *corev1.Pod
		expected bool
	}{
		{
			name: "Pod with annotation",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				return pod1
			},
			expected: true,
		},
		{
			name: "Pod without injected annotation",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", false)
				return pod1
			},
			expected: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {

			actual := hasBeenInjected(tt.pod())
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestProcessUpstreams(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name        string
		pod         func() *corev1.Pod
		expected    []api.Upstream
		expErr      string
		configEntry func() api.ConfigEntry
	}{
		{
			name: "upstream with datacenter without ProxyDefaults",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expErr: "upstream \"upstream1:1234:dc1\" is invalid: there is no ProxyDefaults config to set mesh gateway mode",
		},
		{
			name: "upstream with datacenter with ProxyDefaults whose mesh gateway mode is not local or remote",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expErr: "upstream \"upstream1:1234:dc1\" is invalid: ProxyDefaults mesh gateway mode is neither \"local\" nor \"remote\"",
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "bad-mode"
				return pd
			},
		},
		{
			name: "upstream with datacenter with ProxyDefaults",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					Datacenter:      "dc1",
					LocalBindPort:   1234,
				},
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = api.MeshGatewayModeLocal
				return pd
			},
		},
		{
			name: "multiple upstreams",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234, upstream2:2234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					LocalBindPort:   1234,
				},
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream2",
					LocalBindPort:   2234,
				},
			},
		},
		{
			name: "prepared query upstream",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationUpstreams] = "prepared_query:queryname:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypePreparedQuery,
					DestinationName: "queryname",
					LocalBindPort:   1234,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			if tt.configEntry != nil {
				consulClient.ConfigEntries().Set(tt.configEntry(), &api.WriteOptions{})
			}

			ep := &EndpointsController{
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			}

			upstreams, err := ep.processUpstreams(tt.pod())
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, upstreams)
			}
		})
	}
}

// TestReconcileCreateEndpoint tests the logic to create service instances in Consul from the addresses in the Endpoints
// object. The cases test an empty endpoints object, a basic endpoints object with one address, a basic endpoints object
// with two addresses, and an endpoints object with every possible customization.
func TestReconcileCreateEndpoint(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                       string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*api.AgentServiceRegistration
		expectedNumSvcInstances    int
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
	}{
		{
			name:          "Empty endpoints",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{},
						},
					},
				}
				return []runtime.Object{endpoint}
			},
			initialConsulSvcs:          []*api.AgentServiceRegistration{},
			expectedNumSvcInstances:    0,
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
		},
		{
			name:          "Basic endpoints",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags:    []string{},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod1-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags: []string{},
				},
			},
		},
		{
			name:          "Endpoints with multiple addresses",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod2 := createPod("pod2", "2.2.3.4", true)
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								corev1.EndpointAddress{
									IP:       "2.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpointWithTwoAddresses}
			},
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 2,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags:    []string{},
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags:    []string{},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod1-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags: []string{},
				},
				{
					ServiceID:      "pod2-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "2.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod2-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags: []string{},
				},
			},
		},
		{
			name:          "Every configurable field set: port, different Consul service name, meta, tags, upstreams",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationPort] = "1234"
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				pod1.Annotations[fmt.Sprintf("%sfoo", annotationMeta)] = "bar"
				pod1.Annotations[annotationTags] = "abc,123"
				pod1.Annotations[annotationUpstreams] = "upstream1:1234"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceName:    "different-consul-svc-name",
					ServiceAddress: "1.2.3.4",
					ServicePort:    1234,
					ServiceMeta:    map[string]string{"foo": "bar", MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags:    []string{"abc", "123"},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
					ServiceName:    "different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
						LocalServiceAddress:    "127.0.0.1",
						LocalServicePort:       1234,
						Upstreams: []api.Upstream{
							{
								DestinationType: api.UpstreamDestTypeService,
								DestinationName: "upstream1",
								LocalBindPort:   1234,
							},
						},
					},
					ServiceMeta: map[string]string{"foo": "bar", MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags: []string{"abc", "123"},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), fakeClientPod)
			client := fake.NewFakeClient(k8sObjects...)

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul
			for _, svc := range tt.initialConsulSvcs {
				err = consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			// Create the endpoints controller
			ep := &EndpointsController{
				Client:                client,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
			}
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "service-created",
			}

			resp, err := ep.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have the service with the correct number of instances
			serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
			for i, instance := range serviceInstances {
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceTags, instance.ServiceTags)
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
			for i, instance := range proxyServiceInstances {
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceProxy, instance.ServiceProxy)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceTags, instance.ServiceTags)
			}

			_, checkInfos, err := consulClient.Agent().AgentHealthServiceByName(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName))
			expectedChecks := []string{"Proxy Public Listener", "Destination Alias"}
			require.NoError(t, err)
			require.Len(t, checkInfos, tt.expectedNumSvcInstances)
			for _, checkInfo := range checkInfos {
				checks := checkInfo.Checks
				require.Contains(t, expectedChecks, checks[0].Name)
				require.Contains(t, expectedChecks, checks[1].Name)
			}
		})
	}
}

// Tests updating an Endpoints object.
//   - Tests updates via the register codepath:
//     - When an address in an Endpoint is updated, that the corresponding service instance in Consul is updated.
//     - When an address is added to an Endpoint, an additional service instance in Consul is registered.
//   - Tests updates via the deregister codepath:
//     - When an address is removed from an Endpoint, the corresponding service instance in Consul is deregistered.
//     - When an address is removed from an Endpoint *and there are no addresses left in the Endpoint*, the
//     corresponding service instance in Consul is deregistered.
// For the register and deregister codepath, this also tests that they work when the Consul service name is different
// from the K8s service name.
func TestReconcileUpdateEndpoint(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                       string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*api.AgentServiceRegistration
		expectedNumSvcInstances    int
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
	}{
		{
			name:          "Endpoints has an updated address (pod IP change).",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "4.4.4.4", true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "4.4.4.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
					},
				},
			},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "4.4.4.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "4.4.4.4",
				},
			},
		},
		{
			name:          "Different Consul service name: Endpoints has an updated address (pod IP change).",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "4.4.4.4", true)
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "4.4.4.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
				},
			},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceAddress: "4.4.4.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "4.4.4.4",
				},
			},
		},
		{
			name:          "Endpoints has additional address not in Consul.",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod2 := createPod("pod2", "2.2.3.4", true)
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								corev1.EndpointAddress{
									IP:       "2.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpointWithTwoAddresses}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
					},
				},
			},
			expectedNumSvcInstances: 2,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
				{
					ServiceID:      "pod2-service-updated",
					ServiceAddress: "2.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
				{
					ServiceID:      "pod2-service-updated-sidecar-proxy",
					ServiceAddress: "2.2.3.4",
				},
			},
		},
		{
			name:          "Consul has instances that are not in the Endpoints addresses.",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod2-service-updated",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
			},
		},
		{
			name:          "Different Consul service name: Consul has instances that are not in the Endpoints addresses.",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod2-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
			},
		},
		{
			// When a k8s deployment is deleted but it's k8s service continues to exist, the endpoints has no addresses.
			name:          "Consul has instances that are not in the endpoints, and the endpoints has no addresses.",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod2-service-updated",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances:    0,
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
		},
		{
			// When a k8s deployment is deleted but it's k8s service continues to exist, the endpoints has no addresses.
			name:          "Different Consul service name: Consul has instances that are not in the endpoints, and the endpoints has no addresses.",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod2-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances:    0,
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), fakeClientPod)
			client := fake.NewFakeClient(k8sObjects...)

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul
			for _, svc := range tt.initialConsulSvcs {
				err = consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			// Create the endpoints controller
			ep := &EndpointsController{
				Client:                client,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
			}
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "service-updated",
			}

			resp, err := ep.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have service-updated with the correct number of instances
			serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
			for i, instance := range serviceInstances {
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
			for i, instance := range proxyServiceInstances {
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
			}
		})
	}
}

// Tests deleting an Endpoints object, with and without matching Consul and K8s service names.
func TestReconcileDeleteEndpoint(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name              string
		consulSvcName     string
		initialConsulSvcs []*api.AgentServiceRegistration
	}{
		{
			name:          "Consul service name matches K8s service name",
			consulSvcName: "service-deleted",
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-deleted",
					Name:    "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Name:    "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
			},
		},
		{
			name:          "Consul service name does not match K8s service name",
			consulSvcName: "different-consul-svc-name",
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Create fake k8s client
			client := fake.NewFakeClient(fakeClientPod)

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul
			for _, svc := range tt.initialConsulSvcs {
				err = consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			// Create the endpoints controller
			ep := &EndpointsController{
				Client:                client,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
			}

			// Set up the Endpoint that will be reconciled, and reconcile
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "service-deleted",
			}
			resp, err := ep.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should not have any instances of service-deleted
			serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
			require.NoError(t, err)
			require.Empty(t, serviceInstances)
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Empty(t, proxyServiceInstances)

		})
	}
}

func createPod(name, ip string, inject bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: "127.0.0.1",
		},
	}
	if inject {
		pod.Labels[labelInject] = injected
		pod.Annotations[annotationStatus] = injected
	}
	return pod

}
