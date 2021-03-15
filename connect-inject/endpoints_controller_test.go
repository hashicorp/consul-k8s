package connectinject

import (
	"fmt"
	"strings"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// First test existing code, then:
// Look at container_init_test where we do service registration to see what we're missing
// i.e tags, processing dc on upstream, prepared queries

// How to not forever requeue
// TODO handle a case when the name has been overwritten by the annotation
// in delete, we would get the k8s svc name from metadata on svc instance
// in create/update, we would get it from the pod annotation !!!
// Always query based on metadata rather than name
// To filter based on name, we need to either use the Agent endpoint on every agent or query all services and then use service with the query meta

// TODO we don't want to manually requeue, as it will get requeued on errors

// TODO test error cases like if it failed to get services matching the meta filter

func TestAllowDenyNS(t *testing.T) {
	// tested allow * in other tests, so this needs to be cases when this doesn't work
}
func TestHasBeenInjected(t *testing.T) {
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

// Test create make sure to have a test that asserts against all fields
// TODO basic service
// TODO service with a port
// TODO service with upstreams
// TODO k8s-svc-name different from svc name/svc name annotation
func TestReconcileCreateEndpoint(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name                       string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*capi.AgentServiceRegistration
		expectedNumSvcInstances    int
		expectedConsulSvcInstances []*capi.CatalogService
		expectedProxySvcInstances  []*capi.CatalogService
	}{
		{
			name:          "New basic endpoints object added: every config field: port, TBD",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationPort] = "1234"
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				pod2 := createPod("pod2", "2.2.3.4", true)
				pod2.Annotations[annotationPort] = "1234"
				pod2.Annotations[annotationService] = "different-consul-svc-name"
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
			initialConsulSvcs:       []*capi.AgentServiceRegistration{},
			expectedNumSvcInstances: 2,
			expectedConsulSvcInstances: []*capi.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceName:    "different-consul-svc-name",
					ServiceAddress: "1.2.3.4",
					ServicePort:    1234,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
				},
				{
					ServiceID:      "pod2-different-consul-svc-name",
					ServiceName:    "different-consul-svc-name",
					ServiceAddress: "2.2.3.4",
					ServicePort:    1234,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
				},
			},
			expectedProxySvcInstances: []*capi.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
						LocalServiceAddress:    "127.0.0.1",
						LocalServicePort:       1234,
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
				},
				{
					ServiceID:      "pod2-different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "2.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod2-different-consul-svc-name",
						LocalServiceAddress:    "127.0.0.1",
						LocalServicePort:       1234,
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
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
			fakeClientPod.Labels = map[string]string{"component": "client"}

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
			consulClient, err := capi.NewClient(&capi.Config{
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
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceMeta, instance.ServiceMeta)
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
			for i, instance := range proxyServiceInstances {
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceProxy, instance.ServiceProxy)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceMeta, instance.ServiceMeta)
			}
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

// TODO Different k8s svc name
// TODO Update an IP in an address
func TestReconcileUpdateEndpoint(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name                       string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*capi.AgentServiceRegistration
		expectedNumSvcInstances    int
		expectedConsulSvcInstances []*capi.CatalogService
		expectedProxySvcInstances  []*capi.CatalogService
	}{
		{
			name: "Endpoints has additional address not in Consul.",
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
			initialConsulSvcs: []*capi.AgentServiceRegistration{
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
			expectedConsulSvcInstances: []*capi.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
				{
					ServiceID:      "pod2-service-updated",
					ServiceAddress: "2.2.3.4",
				},
			},
			expectedProxySvcInstances: []*capi.CatalogService{
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
			name: "Endpoints does not have addresses that are in Consul.",
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
			initialConsulSvcs: []*capi.AgentServiceRegistration{
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
			expectedConsulSvcInstances: []*capi.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedProxySvcInstances: []*capi.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
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
			fakeClientPod.Labels = map[string]string{"component": "client"}

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
			consulClient, err := capi.NewClient(&capi.Config{
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
			serviceInstances, _, err := consulClient.Catalog().Service("service-updated", "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
			for i, instance := range serviceInstances {
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service("service-updated-sidecar-proxy", "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
			for i, instance := range proxyServiceInstances {
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
			}
		})
	}
}

func TestReconcileDeleteEndpoint(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name                       string
		k8sSvcName                 string
		initialConsulSvcs          []*capi.AgentServiceRegistration
		expectedConsulSvcInstances []*capi.CatalogService
		expectedProxySvcInstances  []*capi.CatalogService
	}{
		{
			name:       "K8s service name matches Consul service name",
			k8sSvcName: "service-deleted",
			initialConsulSvcs: []*capi.AgentServiceRegistration{
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
			expectedConsulSvcInstances: []*capi.CatalogService{},
			expectedProxySvcInstances:  []*capi.CatalogService{},
		},
		{
			name:       "K8s service name does not match Consul service name",
			k8sSvcName: "different-k8s-svc-name",
			initialConsulSvcs: []*capi.AgentServiceRegistration{
				{
					ID:      "pod1-service-deleted",
					Name:    "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "different-k8s-svc-name", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "different-k8s-svc-name", "k8s-namespace": "default"},
				},
			},
			expectedConsulSvcInstances: []*capi.CatalogService{},
			expectedProxySvcInstances:  []*capi.CatalogService{},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false)
			fakeClientPod.Labels = map[string]string{"component": "client"}

			// Create fake k8s client
			client := fake.NewFakeClient(fakeClientPod)

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForLeader(t)
			consulClient, err := capi.NewClient(&capi.Config{
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
			}

			// Set up the Endpoint that will be reconciled, and reconcile
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      tt.k8sSvcName,
			}
			resp, err := ep.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should not have any instances of service-deleted
			serviceInstances, _, err := consulClient.Catalog().Service("service-deleted", "", nil)
			require.NoError(t, err)
			require.Empty(t, serviceInstances)
			proxyServiceInstances, _, err := consulClient.Catalog().Service("service-deleted-sidecar-proxy", "", nil)
			require.NoError(t, err)
			require.Empty(t, proxyServiceInstances)

		})
	}
}
