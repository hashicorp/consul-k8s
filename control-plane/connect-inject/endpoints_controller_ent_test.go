//go:build enterprise

package connectinject

import (
	"context"
	"fmt"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

// TestReconcileCreateEndpoint tests the logic to create service instances in Consul from the addresses in the Endpoints
// object. The cases test a basic endpoints object with two addresses. This test verifies that the services and their TTL
// health checks are created in the expected Consul namespace for various combinations of namespace flags.
// This test covers EndpointsController.createServiceRegistrations.
func TestReconcileCreateEndpointWithNamespaces(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}
	for name, testCase := range cases {
		setup := struct {
			consulSvcName              string
			k8sObjects                 func() []runtime.Object
			expectedConsulSvcInstances []*api.CatalogService
			expectedProxySvcInstances  []*api.CatalogService
			expectedHealthChecks       []*api.HealthCheck
		}{
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPodWithNamespace("pod1", testCase.SourceKubeNS, "1.2.3.4", true, true)
				pod2 := createPodWithNamespace("pod2", testCase.SourceKubeNS, "2.2.3.4", true, true)
				meshGateway := createGatewayWithNamespace("mesh-gateway", testCase.SourceKubeNS, "3.3.3.3", map[string]string{
					annotationMeshGatewaySource:        "Static",
					annotationMeshGatewayWANAddress:    "2.3.4.5",
					annotationMeshGatewayWANPort:       "443",
					annotationMeshGatewayContainerPort: "8443",
					annotationGatewayKind:              "mesh"})
				endpointWithAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: testCase.SourceKubeNS,
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: testCase.SourceKubeNS,
									},
								},
								{
									IP: "2.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: testCase.SourceKubeNS,
									},
								},
								{
									IP: "3.3.3.3",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "mesh-gateway",
										Namespace: testCase.SourceKubeNS,
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, meshGateway, endpointWithAddresses}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: testCase.SourceKubeNS, MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
					Namespace:      testCase.ExpConsulNS,
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: testCase.SourceKubeNS, MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
					Namespace:      testCase.ExpConsulNS,
				},
				{
					ServiceID:      "mesh-gateway",
					ServiceName:    "mesh-gateway",
					ServiceAddress: "3.3.3.3",
					ServiceMeta:    map[string]string{MetaKeyPodName: "mesh-gateway", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: testCase.SourceKubeNS, MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
					ServicePort:    8443,
					ServiceTaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "3.3.3.3",
							Port:    8443,
						},
						"wan": {
							Address: "2.3.4.5",
							Port:    443,
						},
					},
					Namespace: "default",
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
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: testCase.SourceKubeNS, MetaKeyManagedBy: managedByValue},
					ServiceTags: []string{},
					Namespace:   testCase.ExpConsulNS,
				},
				{
					ServiceID:      "pod2-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "2.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod2-service-created",
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: testCase.SourceKubeNS, MetaKeyManagedBy: managedByValue},
					ServiceTags: []string{},
					Namespace:   testCase.ExpConsulNS,
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     fmt.Sprintf("%s/pod1-service-created", testCase.SourceKubeNS),
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
				{
					CheckID:     fmt.Sprintf("%s/pod1-service-created-sidecar-proxy", testCase.SourceKubeNS),
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod1-service-created-sidecar-proxy",
					Name:        ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
				{
					CheckID:     fmt.Sprintf("%s/pod2-service-created", testCase.SourceKubeNS),
					ServiceName: "service-created",
					ServiceID:   "pod2-service-created",
					Name:        ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
				{
					CheckID:     fmt.Sprintf("%s/pod2-service-created-sidecar-proxy", testCase.SourceKubeNS),
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod2-service-created-sidecar-proxy",
					Name:        ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
				{
					CheckID:     fmt.Sprintf("%s/mesh-gateway", testCase.SourceKubeNS),
					ServiceName: "mesh-gateway",
					ServiceID:   "mesh-gateway",
					Name:        ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ConsulKubernetesCheckType,
					Namespace:   "default",
				},
			},
		}
		t.Run(name, func(t *testing.T) {
			// Add the pods namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testCase.SourceKubeNS}}
			// Create fake k8s client.
			k8sObjects := append(setup.k8sObjects(), &ns)
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create testCase Consul server.
			consul, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForLeader(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			_, err = namespaces.EnsureExists(consulClient, testCase.ExpConsulNS, "")
			require.NoError(t, err)

			// Create the endpoints controller.
			ep := &EndpointsController{
				Client:                     fakeClient,
				Log:                        logrtest.TestLogger{T: t},
				ConsulClient:               consulClient,
				AllowK8sNamespacesSet:      mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:       mapset.NewSetWith(),
				ReleaseName:                "consul",
				ReleaseNamespace:           "default",
				EnableConsulNamespaces:     true,
				ConsulDestinationNamespace: testCase.DestConsulNS,
				EnableNSMirroring:          testCase.Mirror,
				NSMirroringPrefix:          testCase.MirrorPrefix,
			}
			namespacedName := types.NamespacedName{
				Namespace: testCase.SourceKubeNS,
				Name:      "service-created",
			}

			resp, err := ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			cfg.Namespace = testCase.ExpConsulNS
			consulClient, err = api.NewClient(cfg)
			require.NoError(t, err)
			// After reconciliation, Consul should have the service with the correct number of instances.
			serviceInstances, _, err := consulClient.Catalog().Service(setup.consulSvcName, "", &api.QueryOptions{Namespace: testCase.ExpConsulNS})
			require.NoError(t, err)
			service, _, err := consulClient.Catalog().Service("mesh-gateway", "", &api.QueryOptions{Namespace: "default"})
			require.NoError(t, err)
			serviceInstances = append(serviceInstances, service...)
			require.Len(t, serviceInstances, len(setup.expectedConsulSvcInstances))
			for i, instance := range serviceInstances {
				require.Equal(t, setup.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, setup.expectedConsulSvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, setup.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, setup.expectedConsulSvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, setup.expectedConsulSvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, setup.expectedConsulSvcInstances[i].ServiceTags, instance.ServiceTags)
				require.Equal(t, setup.expectedConsulSvcInstances[i].ServiceTaggedAddresses, instance.ServiceTaggedAddresses)
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", setup.consulSvcName), "", &api.QueryOptions{
				Namespace: testCase.ExpConsulNS,
			})
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, len(setup.expectedProxySvcInstances))
			for i, instance := range proxyServiceInstances {
				require.Equal(t, setup.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, setup.expectedProxySvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, setup.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, setup.expectedProxySvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, setup.expectedProxySvcInstances[i].ServiceProxy, instance.ServiceProxy)
				require.Equal(t, setup.expectedProxySvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, setup.expectedProxySvcInstances[i].ServiceTags, instance.ServiceTags)
			}

			// Check that the Consul health checks was created for the k8s pod.
			for _, expectedCheck := range setup.expectedHealthChecks {
				var checks api.HealthChecks
				filter := fmt.Sprintf("CheckID == `%s`", expectedCheck.CheckID)
				checks, _, err := consulClient.Health().Checks(expectedCheck.ServiceName, &api.QueryOptions{Filter: filter})
				if expectedCheck.ServiceName == "mesh-gateway" {
					checks, _, err = consulClient.Health().Checks("mesh-gateway", &api.QueryOptions{Namespace: "default"})
					require.NoError(t, err)
				}
				require.NoError(t, err)
				require.Equal(t, len(checks), 1)
				var ignoredFields = []string{"Node", "Definition", "Partition", "CreateIndex", "ModifyIndex", "ServiceTags"}
				require.True(t, cmp.Equal(checks[0], expectedCheck, cmpopts.IgnoreFields(api.HealthCheck{}, ignoredFields...)))
			}
		})
	}
}

// Tests updating an Endpoints object when Consul namespaces are enabled.
//   - Tests updates via the register codepath:
//     - When an address in an Endpoint is updated, that the corresponding service instance in Consul is updated in the correct Consul namespace.
//     - When an address is added to an Endpoint, an additional service instance in Consul is registered in the correct Consul namespace.
//   - Tests updates via the deregister codepath:
//     - When an address is removed from an Endpoint, the corresponding service instance in Consul is deregistered.
//     - When an address is removed from an Endpoint *and there are no addresses left in the Endpoint*, the
//     corresponding service instance in Consul is deregistered.
// For the register and deregister codepath, this also tests that they work when the Consul service name is different
// from the K8s service name.
// This test covers EndpointsController.deregisterService when services should be selectively deregistered
// since the map will not be nil.
func TestReconcileUpdateEndpointWithNamespaces(t *testing.T) {
	t.Parallel()
	nsCases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}
	for name, ts := range nsCases {
		cases := []struct {
			name                       string
			consulSvcName              string
			k8sObjects                 func() []runtime.Object
			initialConsulSvcs          []*api.CatalogRegistration
			expectedConsulSvcInstances []*api.CatalogService
			expectedProxySvcInstances  []*api.CatalogService
			enableACLs                 bool
		}{
			{
				name:          "Endpoints has an updated address (pod IP change).",
				consulSvcName: "service-updated",
				k8sObjects: func() []runtime.Object {
					pod1 := createPodWithNamespace("pod1", ts.SourceKubeNS, "4.4.4.4", true, true)
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
						Subsets: []corev1.EndpointSubset{
							{
								Addresses: []corev1.EndpointAddress{
									{
										IP: "4.4.4.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod1",
											Namespace: ts.SourceKubeNS,
										},
									},
								},
							},
						},
					}
					return []runtime.Object{pod1, endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod1-service-updated",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated",
						ServiceAddress: "4.4.4.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				expectedProxySvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated-sidecar-proxy",
						ServiceAddress: "4.4.4.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
			},
			{
				name:          "Different Consul service name: Endpoints has an updated address (pod IP change).",
				consulSvcName: "different-consul-svc-name",
				k8sObjects: func() []runtime.Object {
					pod1 := createPodWithNamespace("pod1", ts.SourceKubeNS, "4.4.4.4", true, true)
					pod1.Annotations[annotationService] = "different-consul-svc-name"
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
						Subsets: []corev1.EndpointSubset{
							{
								Addresses: []corev1.EndpointAddress{
									{
										IP: "4.4.4.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod1",
											Namespace: ts.SourceKubeNS,
										},
									},
								},
							},
						},
					}
					return []runtime.Object{pod1, endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							ID:        "pod1-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-different-consul-svc-name-sidecar-proxy",
							Service: "different-consul-svc-name-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "different-consul-svc-name",
								DestinationServiceID:   "pod1-different-consul-svc-name",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-different-consul-svc-name",
						ServiceAddress: "4.4.4.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				expectedProxySvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
						ServiceAddress: "4.4.4.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
			},
			{
				name:          "Endpoints has additional address not in Consul.",
				consulSvcName: "service-updated",
				k8sObjects: func() []runtime.Object {
					pod1 := createPodWithNamespace("pod1", ts.SourceKubeNS, "1.2.3.4", true, true)
					pod2 := createPodWithNamespace("pod2", ts.SourceKubeNS, "2.2.3.4", true, true)
					endpointWithTwoAddresses := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
						Subsets: []corev1.EndpointSubset{
							{
								Addresses: []corev1.EndpointAddress{
									{
										IP: "1.2.3.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod1",
											Namespace: ts.SourceKubeNS,
										},
									},
									{
										IP: "2.2.3.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod2",
											Namespace: ts.SourceKubeNS,
										},
									},
								},
							},
						},
					}
					return []runtime.Object{pod1, pod2, endpointWithTwoAddresses}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod1-service-updated",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
					{
						ServiceID:      "pod2-service-updated",
						ServiceAddress: "2.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				expectedProxySvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated-sidecar-proxy",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
					{
						ServiceID:      "pod2-service-updated-sidecar-proxy",
						ServiceAddress: "2.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
			},
			{
				name:          "Consul has instances that are not in the Endpoints addresses",
				consulSvcName: "service-updated",
				k8sObjects: func() []runtime.Object {
					pod1 := createPodWithNamespace("pod1", ts.SourceKubeNS, "1.2.3.4", true, true)
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
						Subsets: []corev1.EndpointSubset{
							{
								Addresses: []corev1.EndpointAddress{
									{
										IP: "1.2.3.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod1",
											Namespace: ts.SourceKubeNS,
										},
									},
								},
							},
						},
					}
					return []runtime.Object{pod1, endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod1-service-updated",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod2-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod2-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "2.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod2-service-updated",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				expectedProxySvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated-sidecar-proxy",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
			},
			{
				name:          "Different Consul service name: Consul has instances that are not in the Endpoints addresses",
				consulSvcName: "different-consul-svc-name",
				k8sObjects: func() []runtime.Object {
					pod1 := createPodWithNamespace("pod1", ts.SourceKubeNS, "1.2.3.4", true, true)
					pod1.Annotations[annotationService] = "different-consul-svc-name"
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
						Subsets: []corev1.EndpointSubset{
							{
								Addresses: []corev1.EndpointAddress{
									{
										IP: "1.2.3.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod1",
											Namespace: ts.SourceKubeNS,
										},
									},
								},
							},
						},
					}
					return []runtime.Object{pod1, endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod1-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-different-consul-svc-name-sidecar-proxy",
							Service: "different-consul-svc-name-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "different-consul-svc-name",
								DestinationServiceID:   "pod1-different-consul-svc-name",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod2-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod2-different-consul-svc-name-sidecar-proxy",
							Service: "different-consul-svc-name-sidecar-proxy",
							Port:    20000,
							Address: "2.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "different-consul-svc-name",
								DestinationServiceID:   "pod2-different-consul-svc-name",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-different-consul-svc-name",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				expectedProxySvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
			},
			{
				// When a k8s deployment is deleted but it's k8s service continues to exist, the endpoints has no addresses
				// and the instances should be deleted from Consul.
				name:          "Consul has instances that are not in the endpoints, and the endpoints has no addresses.",
				consulSvcName: "service-updated",
				k8sObjects: func() []runtime.Object {
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
					}
					return []runtime.Object{endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod1-service-updated",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod2-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod2-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "2.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod2-service-updated",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{},
				expectedProxySvcInstances:  []*api.CatalogService{},
			},
			{
				// With a different Consul service name, when a k8s deployment is deleted but it's k8s service continues to
				// exist, the endpoints has no addresses and the instances should be deleted from Consul.
				name:          "Different Consul service name: Consul has instances that are not in the endpoints, and the endpoints has no addresses.",
				consulSvcName: "different-consul-svc-name",
				k8sObjects: func() []runtime.Object {
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
					}
					return []runtime.Object{endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod1-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-different-consul-svc-name-sidecar-proxy",
							Service: "different-consul-svc-name-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "different-consul-svc-name",
								DestinationServiceID:   "pod1-different-consul-svc-name",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:        "pod2-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod2-different-consul-svc-name-sidecar-proxy",
							Service: "different-consul-svc-name-sidecar-proxy",
							Port:    20000,
							Address: "2.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "different-consul-svc-name",
								DestinationServiceID:   "pod2-different-consul-svc-name",
							},
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{},
				expectedProxySvcInstances:  []*api.CatalogService{},
			},
			{
				name:          "ACLs enabled: Endpoints has an updated address because the target pod changes",
				consulSvcName: "service-updated",
				k8sObjects: func() []runtime.Object {
					pod2 := createPodWithNamespace("pod2", ts.SourceKubeNS, "4.4.4.4", true, true)
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
						Subsets: []corev1.EndpointSubset{
							{
								Addresses: []corev1.EndpointAddress{
									{
										IP: "4.4.4.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod2",
											Namespace: ts.SourceKubeNS,
										},
									},
								},
							},
						},
					}
					return []runtime.Object{pod2, endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:      "pod1-service-updated",
							Service: "service-updated",
							Port:    80,
							Address: "1.2.3.4",
							Meta: map[string]string{
								MetaKeyManagedBy:       managedByValue,
								MetaKeyKubeServiceName: "service-updated",
								MetaKeyPodName:         "pod1",
								MetaKeyKubeNS:          ts.SourceKubeNS,
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod1-service-updated",
							},
							Meta: map[string]string{
								MetaKeyManagedBy:       managedByValue,
								MetaKeyKubeServiceName: "service-updated",
								MetaKeyPodName:         "pod1",
								MetaKeyKubeNS:          ts.SourceKubeNS,
							},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod2-service-updated",
						ServiceAddress: "4.4.4.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				expectedProxySvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod2-service-updated-sidecar-proxy",
						ServiceAddress: "4.4.4.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				enableACLs: true,
			},
			{
				name:          "ACLs enabled: Consul has instances that are not in the Endpoints addresses",
				consulSvcName: "service-updated",
				k8sObjects: func() []runtime.Object {
					pod1 := createPodWithNamespace("pod1", ts.SourceKubeNS, "1.2.3.4", true, true)
					endpoint := &corev1.Endpoints{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service-updated",
							Namespace: ts.SourceKubeNS,
						},
						Subsets: []corev1.EndpointSubset{
							{
								Addresses: []corev1.EndpointAddress{
									{
										IP: "1.2.3.4",
										TargetRef: &corev1.ObjectReference{
											Kind:      "Pod",
											Name:      "pod1",
											Namespace: ts.SourceKubeNS,
										},
									},
								},
							},
						},
					}
					return []runtime.Object{pod1, endpoint}
				},
				initialConsulSvcs: []*api.CatalogRegistration{
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:      "pod1-service-updated",
							Service: "service-updated",
							Port:    80,
							Address: "1.2.3.4",
							Meta: map[string]string{
								MetaKeyKubeServiceName: "service-updated",
								MetaKeyKubeNS:          ts.SourceKubeNS,
								MetaKeyManagedBy:       managedByValue,
								MetaKeyPodName:         "pod1",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod1-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "1.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod1-service-updated",
							},
							Meta: map[string]string{
								MetaKeyKubeServiceName: "service-updated",
								MetaKeyKubeNS:          ts.SourceKubeNS,
								MetaKeyManagedBy:       managedByValue,
								MetaKeyPodName:         "pod1",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: "127.0.0.1",
						Service: &api.AgentService{
							ID:      "pod2-service-updated",
							Service: "service-updated",
							Port:    80,
							Address: "2.2.3.4",
							Meta: map[string]string{
								MetaKeyKubeServiceName: "service-updated",
								MetaKeyKubeNS:          ts.SourceKubeNS,
								MetaKeyManagedBy:       managedByValue,
								MetaKeyPodName:         "pod2",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: &api.AgentService{
							Kind:    api.ServiceKindConnectProxy,
							ID:      "pod2-service-updated-sidecar-proxy",
							Service: "service-updated-sidecar-proxy",
							Port:    20000,
							Address: "2.2.3.4",
							Proxy: &api.AgentServiceConnectProxyConfig{
								DestinationServiceName: "service-updated",
								DestinationServiceID:   "pod2-service-updated",
							},
							Meta: map[string]string{
								MetaKeyKubeServiceName: "service-updated",
								MetaKeyKubeNS:          ts.SourceKubeNS,
								MetaKeyManagedBy:       managedByValue,
								MetaKeyPodName:         "pod2",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
				},
				expectedConsulSvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				expectedProxySvcInstances: []*api.CatalogService{
					{
						ServiceID:      "pod1-service-updated-sidecar-proxy",
						ServiceAddress: "1.2.3.4",
						Namespace:      ts.ExpConsulNS,
					},
				},
				enableACLs: true,
			},
		}
		for _, tt := range cases {
			t.Run(fmt.Sprintf("%s: %s", name, tt.name), func(t *testing.T) {
				// Add the pods namespace.
				ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ts.SourceKubeNS}}
				// Create fake k8s client.
				k8sObjects := append(tt.k8sObjects(), &ns)
				fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

				adminToken := "123e4567-e89b-12d3-a456-426614174000"
				consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
					if tt.enableACLs {
						c.ACL.Enabled = true
						c.ACL.Tokens.InitialManagement = adminToken
					}
				})
				require.NoError(t, err)
				defer consul.Stop()
				consul.WaitForSerfCheck(t)

				cfg := &api.Config{
					Scheme:  "http",
					Address: consul.HTTPAddr,
				}
				if tt.enableACLs {
					cfg.Token = adminToken
				}

				consulClient, err := api.NewClient(cfg)
				require.NoError(t, err)

				_, err = namespaces.EnsureExists(consulClient, ts.ExpConsulNS, "")
				require.NoError(t, err)

				// Holds token accessorID for each service ID.
				tokensForServices := make(map[string]string)

				// Register service and proxy in Consul.
				for _, svc := range tt.initialConsulSvcs {
					_, err = consulClient.Catalog().Register(svc, nil)
					require.NoError(t, err)
					// Create a token for this service if ACLs are enabled.
					if tt.enableACLs {
						if svc.Service.Kind != api.ServiceKindConnectProxy {
							var writeOpts api.WriteOptions
							// When mirroring is enabled, the auth method will be created in the "default" Consul namespace.
							if ts.Mirror {
								writeOpts.Namespace = "default"
							} else {
								writeOpts.Namespace = ts.ExpConsulNS
							}
							test.SetupK8sAuthMethodWithNamespaces(t, consulClient, svc.Service.Service, svc.Service.Meta[MetaKeyKubeNS], ts.ExpConsulNS, ts.Mirror, ts.MirrorPrefix)
							token, _, err := consulClient.ACL().Login(&api.ACLLoginParams{
								AuthMethod:  test.AuthMethod,
								BearerToken: test.ServiceAccountJWTToken,
								Meta: map[string]string{
									TokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Service.Meta[MetaKeyKubeNS], svc.Service.Meta[MetaKeyPodName]),
								},
							}, &writeOpts)

							require.NoError(t, err)

							tokensForServices[svc.ID] = token.AccessorID

							// Create another token for the same service but a pod that either no longer exists
							// or the endpoints controller doesn't know about it yet.
							// This is to test a scenario with either orphaned tokens
							// or tokens for services that haven't yet been registered with Consul.
							// In that case, we have a token for the pod but the service instance
							// for that pod either no longer exists or is not yet registered in Consul.
							// This token should not be deleted.
							token, _, err = consulClient.ACL().Login(&api.ACLLoginParams{
								AuthMethod:  test.AuthMethod,
								BearerToken: test.ServiceAccountJWTToken,
								Meta: map[string]string{
									TokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Service.Meta[MetaKeyKubeNS], "does-not-exist"),
								},
							}, &writeOpts)
							require.NoError(t, err)
							tokensForServices["does-not-exist"+svc.Service.Service] = token.AccessorID
						}
					}
				}

				// Create the endpoints controller.
				ep := &EndpointsController{
					Client:                     fakeClient,
					Log:                        logrtest.TestLogger{T: t},
					ConsulClient:               consulClient,
					AllowK8sNamespacesSet:      mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:       mapset.NewSetWith(),
					ReleaseName:                "consul",
					ReleaseNamespace:           "default",
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          ts.Mirror,
					NSMirroringPrefix:          ts.MirrorPrefix,
					ConsulDestinationNamespace: ts.DestConsulNS,
				}
				if tt.enableACLs {
					ep.AuthMethod = test.AuthMethod
				}
				namespacedName := types.NamespacedName{
					Namespace: ts.SourceKubeNS,
					Name:      "service-updated",
				}

				resp, err := ep.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: namespacedName,
				})
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				// Create new consul client with the expected consul ns so we can make calls for assertions.
				cfg.Namespace = ts.ExpConsulNS
				consulClient, err = api.NewClient(cfg)
				require.NoError(t, err)

				// After reconciliation, Consul should have service-updated with the correct number of instances.
				serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", &api.QueryOptions{Namespace: ts.ExpConsulNS})
				require.NoError(t, err)
				require.Len(t, serviceInstances, len(tt.expectedProxySvcInstances))
				for i, instance := range serviceInstances {
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				}
				proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", &api.QueryOptions{Namespace: ts.ExpConsulNS})
				require.NoError(t, err)
				require.Len(t, proxyServiceInstances, len(tt.expectedProxySvcInstances))
				for i, instance := range proxyServiceInstances {
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
				}

				if tt.enableACLs {
					// Put expected services into a map to make it easier to find service IDs.
					expectedServices := mapset.NewSet()
					for _, svc := range tt.expectedConsulSvcInstances {
						expectedServices.Add(svc.ServiceID)
					}

					initialServices := mapset.NewSet()
					for _, svc := range tt.initialConsulSvcs {
						initialServices.Add(svc.ID)
					}

					// We only care about a case when services are deregistered, where
					// the set of initial services is bigger than the set of expected services.
					deregisteredServices := initialServices.Difference(expectedServices)

					// Look through the tokens we've created and check that only
					// tokens for the deregistered services have been deleted.
					for serviceID, tokenID := range tokensForServices {
						// Read the token from Consul.
						token, _, err := consulClient.ACL().TokenRead(tokenID, nil)
						if deregisteredServices.Contains(serviceID) {
							require.EqualError(t, err, "Unexpected response code: 403 (ACL not found)")
						} else {
							require.NoError(t, err, "token should exist for service instance: "+serviceID)
							require.NotNil(t, token)
						}
					}
				}
			})
		}
	}
}

// Tests deleting an Endpoints object, with and without matching Consul and K8s service names when Consul namespaces are enabled.
// This test covers EndpointsController.deregisterService when the map is nil (not selectively deregistered).
func TestReconcileDeleteEndpointWithNamespaces(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}
	for name, ts := range cases {
		cases := []struct {
			name              string
			consulSvcName     string
			initialConsulSvcs []*api.AgentService
			enableACLs        bool
		}{
			{
				name:          "Consul service name matches K8s service name",
				consulSvcName: "service-deleted",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:        "pod1-service-deleted",
						Service:   "service-deleted",
						Port:      80,
						Address:   "1.2.3.4",
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
						Namespace: ts.ExpConsulNS,
					},
					{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-deleted-sidecar-proxy",
						Service: "service-deleted-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-deleted",
							DestinationServiceID:   "pod1-service-deleted",
						},
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
						Namespace: ts.ExpConsulNS,
					},
				},
			},
			{
				name:          "Consul service name does not match K8s service name",
				consulSvcName: "different-consul-svc-name",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:        "pod1-different-consul-svc-name",
						Service:   "different-consul-svc-name",
						Port:      80,
						Address:   "1.2.3.4",
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
						Namespace: ts.ExpConsulNS,
					},
					{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-different-consul-svc-name-sidecar-proxy",
						Service: "different-consul-svc-name-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "different-consul-svc-name",
							DestinationServiceID:   "pod1-different-consul-svc-name",
							TransparentProxy:       &api.TransparentProxyConfig{},
						},
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, MetaKeyManagedBy: managedByValue},
						Namespace: ts.ExpConsulNS,
					},
				},
			},
			{
				name:          "When ACLs are enabled, the ACL token should be deleted",
				consulSvcName: "service-deleted",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:      "pod1-service-deleted",
						Service: "service-deleted",
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							MetaKeyKubeServiceName: "service-deleted",
							MetaKeyKubeNS:          ts.SourceKubeNS,
							MetaKeyManagedBy:       managedByValue,
							MetaKeyPodName:         "pod1",
						},
						Namespace: ts.ExpConsulNS,
					},
					{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-deleted-sidecar-proxy",
						Service: "service-deleted-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-deleted",
							DestinationServiceID:   "pod1-service-deleted",
						},
						Meta: map[string]string{
							MetaKeyKubeServiceName: "service-deleted",
							MetaKeyKubeNS:          ts.SourceKubeNS,
							MetaKeyManagedBy:       managedByValue,
							MetaKeyPodName:         "pod1",
						},
						Namespace: ts.ExpConsulNS,
					},
				},
				enableACLs: true,
			},
		}
		for _, tt := range cases {
			t.Run(fmt.Sprintf("%s:%s", name, tt.name), func(t *testing.T) {
				// Create fake k8s client.
				fakeClient := fake.NewClientBuilder().Build()

				// Create test Consul server.
				adminToken := "123e4567-e89b-12d3-a456-426614174000"
				consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
					if tt.enableACLs {
						c.ACL.Enabled = true
						c.ACL.Tokens.InitialManagement = adminToken
					}
				})
				require.NoError(t, err)
				defer consul.Stop()

				consul.WaitForLeader(t)
				cfg := &api.Config{
					Address: consul.HTTPAddr,
				}
				if tt.enableACLs {
					cfg.Token = adminToken
				}
				consulClient, err := api.NewClient(cfg)
				require.NoError(t, err)

				_, err = namespaces.EnsureExists(consulClient, ts.ExpConsulNS, "")
				require.NoError(t, err)

				// Register service and proxy in consul.
				var token *api.ACLToken
				for _, svc := range tt.initialConsulSvcs {
					serviceRegistration := &api.CatalogRegistration{
						Node:    ConsulNodeName,
						Address: ConsulNodeAddress,
						Service: svc,
					}
					_, err = consulClient.Catalog().Register(serviceRegistration, nil)
					require.NoError(t, err)
					// Create a token for it if ACLs are enabled.
					if tt.enableACLs {
						if svc.Kind != api.ServiceKindConnectProxy {
							var writeOpts api.WriteOptions
							// When mirroring is enabled, the auth method will be created in the "default" Consul namespace.
							if ts.Mirror {
								writeOpts.Namespace = "default"
							} else {
								writeOpts.Namespace = ts.ExpConsulNS
							}
							test.SetupK8sAuthMethodWithNamespaces(t, consulClient, svc.Service, svc.Meta[MetaKeyKubeNS], ts.ExpConsulNS, ts.Mirror, ts.MirrorPrefix)
							token, _, err = consulClient.ACL().Login(&api.ACLLoginParams{
								AuthMethod:  test.AuthMethod,
								BearerToken: test.ServiceAccountJWTToken,
								Meta: map[string]string{
									TokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Meta[MetaKeyKubeNS], svc.Meta[MetaKeyPodName]),
								},
							}, &writeOpts)

							require.NoError(t, err)
						}
					}
				}

				// Create the endpoints controller.
				ep := &EndpointsController{
					Client:                     fakeClient,
					Log:                        logrtest.TestLogger{T: t},
					ConsulClient:               consulClient,
					AllowK8sNamespacesSet:      mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:       mapset.NewSetWith(),
					ReleaseName:                "consul",
					ReleaseNamespace:           "default",
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          ts.Mirror,
					NSMirroringPrefix:          ts.MirrorPrefix,
					ConsulDestinationNamespace: ts.DestConsulNS,
				}
				if tt.enableACLs {
					ep.AuthMethod = test.AuthMethod
				}

				// Set up the Endpoint that will be reconciled, and reconcile.
				namespacedName := types.NamespacedName{
					Namespace: ts.SourceKubeNS,
					Name:      "service-deleted",
				}
				resp, err := ep.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: namespacedName,
				})
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				cfg.Namespace = ts.ExpConsulNS
				consulClient, err = api.NewClient(cfg)
				require.NoError(t, err)

				// After reconciliation, Consul should not have any instances of service-deleted.
				serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", &api.QueryOptions{Namespace: ts.ExpConsulNS})
				require.NoError(t, err)
				require.Empty(t, serviceInstances)
				proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", &api.QueryOptions{Namespace: ts.ExpConsulNS})
				require.NoError(t, err)
				require.Empty(t, proxyServiceInstances)

				if tt.enableACLs {
					_, _, err = consulClient.ACL().TokenRead(token.AccessorID, nil)
					require.EqualError(t, err, "Unexpected response code: 403 (ACL not found)")
				}
			})
		}
	}
}

func createPodWithNamespace(name, namespace, ip string, inject bool, managedByEndpointsController bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: "127.0.0.1",
			Phase:  corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	if inject {
		pod.Labels[keyInjectStatus] = injected
		pod.Annotations[keyInjectStatus] = injected
	}
	if managedByEndpointsController {
		pod.Labels[keyManagedBy] = managedByValue
	}
	return pod

}

func createGatewayWithNamespace(name, namespace, ip string, annotations map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				keyManagedBy: managedByValue,
			},
			Annotations: annotations,
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: "127.0.0.1",
			Phase:  corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	return pod

}
