// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package endpoints

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

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

// TestReconcileCreateEndpoint tests the logic to create service instances in Consul from the addresses in the Endpoints
// object. The cases test a basic endpoints object with two addresses. This test verifies that the services and their TTL
// health checks are created in the expected Consul namespace for various combinations of namespace flags.
// This test covers Controller.createServiceRegistrations.
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
				endpoints := &corev1.Endpoints{
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
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: testCase.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					Namespace:      testCase.ExpConsulNS,
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod2", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: testCase.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					Namespace:      testCase.ExpConsulNS,
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
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: testCase.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
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
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod2", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: testCase.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
					Namespace:   testCase.ExpConsulNS,
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     fmt.Sprintf("%s/pod1-service-created", testCase.SourceKubeNS),
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
				{
					CheckID:     fmt.Sprintf("%s/pod1-service-created-sidecar-proxy", testCase.SourceKubeNS),
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod1-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
				{
					CheckID:     fmt.Sprintf("%s/pod2-service-created", testCase.SourceKubeNS),
					ServiceName: "service-created",
					ServiceID:   "pod2-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
				{
					CheckID:     fmt.Sprintf("%s/pod2-service-created-sidecar-proxy", testCase.SourceKubeNS),
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod2-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
					Namespace:   testCase.ExpConsulNS,
				},
			},
		}
		t.Run(name, func(t *testing.T) {
			// Add the pods namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testCase.SourceKubeNS}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			// Create fake k8s client.
			k8sObjects := append(setup.k8sObjects(), &ns, &node)
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consulServer server
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)

			_, err := namespaces.EnsureExists(testClient.APIClient, testCase.ExpConsulNS, "")
			require.NoError(t, err)

			// Create the endpoints controller.
			ep := &Controller{
				Client:                     fakeClient,
				Log:                        logrtest.NewTestLogger(t),
				ConsulClientConfig:         testClient.Cfg,
				ConsulServerConnMgr:        testClient.Watcher,
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

			consulConfig := testClient.Cfg
			consulConfig.APIClientConfig.Namespace = testCase.ExpConsulNS
			consulClient, err := api.NewClient(consulConfig.APIClientConfig)
			require.NoError(t, err)

			// After reconciliation, Consul should have the service with the correct number of instances.
			serviceInstances, _, err := consulClient.Catalog().Service(setup.consulSvcName, "", &api.QueryOptions{Namespace: testCase.ExpConsulNS})
			require.NoError(t, err)
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
				require.NoError(t, err)
				require.Equal(t, len(checks), 1)
				var ignoredFields = []string{"Node", "Definition", "Partition", "CreateIndex", "ModifyIndex", "ServiceTags"}
				require.True(t, cmp.Equal(checks[0], expectedCheck, cmpopts.IgnoreFields(api.HealthCheck{}, ignoredFields...)))
			}
		})
	}
}

// TestReconcileCreateGatewayWithNamespaces verifies that gateways created using
// the Endpoints Controller with Consul namespaces are correct.
func TestReconcileCreateGatewayWithNamespaces(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		ConsulNS string
	}{
		"default Consul namespace": {
			ConsulNS: "default",
		},
		"other Consul namespace": {
			ConsulNS: "other",
		},
	}
	for name, testCase := range cases {
		setup := struct {
			k8sObjects                 func() []runtime.Object
			expectedConsulSvcInstances []*api.CatalogService
			expectedProxySvcInstances  []*api.CatalogService
			expectedHealthChecks       []*api.HealthCheck
		}{
			k8sObjects: func() []runtime.Object {
				meshGateway := createGatewayWithNamespace("mesh-gateway", "default", "3.3.3.3", map[string]string{
					constants.AnnotationGatewayWANSource:         "Static",
					constants.AnnotationGatewayWANAddress:        "2.3.4.5",
					constants.AnnotationGatewayWANPort:           "443",
					constants.AnnotationMeshGatewayContainerPort: "8443",
					constants.AnnotationGatewayKind:              meshGateway,
					constants.AnnotationGatewayConsulServiceName: "mesh-gateway"})
				terminatingGateway := createGatewayWithNamespace("terminating-gateway", "default", "4.4.4.4", map[string]string{
					constants.AnnotationGatewayKind:              terminatingGateway,
					constants.AnnotationGatewayNamespace:         testCase.ConsulNS,
					constants.AnnotationGatewayConsulServiceName: "terminating-gateway"})
				ingressGateway := createGatewayWithNamespace("ingress-gateway", "default", "5.5.5.5", map[string]string{
					constants.AnnotationGatewayWANSource:         "Service",
					constants.AnnotationGatewayWANPort:           "8443",
					constants.AnnotationGatewayNamespace:         testCase.ConsulNS,
					constants.AnnotationGatewayKind:              ingressGateway,
					constants.AnnotationGatewayConsulServiceName: "ingress-gateway"})
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "5.6.7.8",
								},
							},
						},
					},
				}
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "3.3.3.3",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "mesh-gateway",
										Namespace: "default",
									},
								},
								{
									IP: "4.4.4.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "terminating-gateway",
										Namespace: "default",
									},
								},
								{
									IP: "5.5.5.5",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "ingress-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{meshGateway, terminatingGateway, ingressGateway, svc, endpoints}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "mesh-gateway",
					ServiceName:    "mesh-gateway",
					ServiceAddress: "3.3.3.3",
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "mesh-gateway", metaKeyKubeServiceName: "gateway", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
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
				{
					ServiceID:      "terminating-gateway",
					ServiceName:    "terminating-gateway",
					ServiceAddress: "4.4.4.4",
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "terminating-gateway", metaKeyKubeServiceName: "gateway", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServicePort:    8443,
					Namespace:      testCase.ConsulNS,
				},
				{
					ServiceID:      "ingress-gateway",
					ServiceName:    "ingress-gateway",
					ServiceAddress: "5.5.5.5",
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "ingress-gateway", metaKeyKubeServiceName: "gateway", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServicePort:    21000,
					ServiceTaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "5.5.5.5",
							Port:    21000,
						},
						"wan": {
							Address: "5.6.7.8",
							Port:    8443,
						},
					},
					Namespace: testCase.ConsulNS,
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/mesh-gateway",
					ServiceName: "mesh-gateway",
					ServiceID:   "mesh-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
					Namespace:   "default",
				},
				{
					CheckID:     "default/terminating-gateway",
					ServiceName: "terminating-gateway",
					ServiceID:   "terminating-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
					Namespace:   testCase.ConsulNS,
				},
				{
					CheckID:     "default/ingress-gateway",
					ServiceName: "ingress-gateway",
					ServiceID:   "ingress-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
					Namespace:   testCase.ConsulNS,
				},
			},
		}
		t.Run(name, func(t *testing.T) {
			// Create fake k8s client.
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(append(setup.k8sObjects(), &node)...).Build()

			// Create testCase Consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient
			_, err := namespaces.EnsureExists(consulClient, testCase.ConsulNS, "")
			require.NoError(t, err)

			// Create the endpoints controller.
			ep := &Controller{
				Client:                 fakeClient,
				Log:                    logrtest.NewTestLogger(t),
				ConsulClientConfig:     testClient.Cfg,
				ConsulServerConnMgr:    testClient.Watcher,
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSetWith(),
				ReleaseName:            "consul",
				ReleaseNamespace:       "default",
				EnableConsulNamespaces: true,
			}
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "gateway",
			}

			resp, err := ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have the service with the correct number of instances.
			var serviceInstances []*api.CatalogService
			for _, expected := range setup.expectedConsulSvcInstances {
				serviceInstance, _, err := consulClient.Catalog().Service(expected.ServiceName, "", &api.QueryOptions{Namespace: expected.Namespace})
				require.NoError(t, err)
				serviceInstances = append(serviceInstances, serviceInstance...)
			}

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

			// Check that the Consul health checks was created for the k8s pod.
			for _, expectedCheck := range setup.expectedHealthChecks {
				var checks api.HealthChecks
				filter := fmt.Sprintf("CheckID == `%s`", expectedCheck.CheckID)
				checks, _, err := consulClient.Health().Checks(expectedCheck.ServiceName, &api.QueryOptions{Filter: filter, Namespace: expectedCheck.Namespace})
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
//   - When an address in an Endpoint is updated, that the corresponding service instance in Consul is updated in the correct Consul namespace.
//   - When an address is added to an Endpoint, an additional service instance in Consul is registered in the correct Consul namespace.
//   - Tests updates via the deregister codepath:
//   - When an address is removed from an Endpoint, the corresponding service instance in Consul is deregistered.
//   - When an address is removed from an Endpoint *and there are no addresses left in the Endpoint*, the
//     corresponding service instance in Consul is deregistered.
//
// For the register and deregister codepath, this also tests that they work when the Consul service name is different
// from the K8s service name.
// This test covers Controller.deregisterService when services should be selectively deregistered
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
					pod1.Annotations[constants.AnnotationService] = "different-consul-svc-name"
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod1-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod2-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
					pod1.Annotations[constants.AnnotationService] = "different-consul-svc-name"
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod1-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod2-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod1-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod2-service-updated",
							Service:   "service-updated",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod1-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "1.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:        "pod2-different-consul-svc-name",
							Service:   "different-consul-svc-name",
							Port:      80,
							Address:   "2.2.3.4",
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
							Meta:      map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:      "pod1-service-updated",
							Service: "service-updated",
							Port:    80,
							Address: "1.2.3.4",
							Meta: map[string]string{
								metaKeyManagedBy:         constants.ManagedByValue,
								metaKeyKubeServiceName:   "service-updated",
								constants.MetaKeyPodName: "pod1",
								constants.MetaKeyKubeNS:  ts.SourceKubeNS,
								metaKeySyntheticNode:     "true",
								constants.MetaKeyPodUID:  "",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
								metaKeyManagedBy:         constants.ManagedByValue,
								metaKeyKubeServiceName:   "service-updated",
								constants.MetaKeyPodName: "pod1",
								constants.MetaKeyKubeNS:  ts.SourceKubeNS,
								metaKeySyntheticNode:     "true",
								constants.MetaKeyPodUID:  "",
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
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:      "pod1-service-updated",
							Service: "service-updated",
							Port:    80,
							Address: "1.2.3.4",
							Meta: map[string]string{
								metaKeyKubeServiceName:   "service-updated",
								constants.MetaKeyKubeNS:  ts.SourceKubeNS,
								metaKeyManagedBy:         constants.ManagedByValue,
								constants.MetaKeyPodName: "pod1",
								metaKeySyntheticNode:     "true",
								constants.MetaKeyPodUID:  "",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
								metaKeyKubeServiceName:   "service-updated",
								constants.MetaKeyKubeNS:  ts.SourceKubeNS,
								metaKeyManagedBy:         constants.ManagedByValue,
								constants.MetaKeyPodName: "pod1",
								metaKeySyntheticNode:     "true",
								constants.MetaKeyPodUID:  "",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
						Service: &api.AgentService{
							ID:      "pod2-service-updated",
							Service: "service-updated",
							Port:    80,
							Address: "2.2.3.4",
							Meta: map[string]string{
								metaKeyKubeServiceName:   "service-updated",
								constants.MetaKeyKubeNS:  ts.SourceKubeNS,
								metaKeyManagedBy:         constants.ManagedByValue,
								constants.MetaKeyPodName: "pod2",
								metaKeySyntheticNode:     "true",
								constants.MetaKeyPodUID:  "",
							},
							Namespace: ts.ExpConsulNS,
						},
					},
					{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
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
								metaKeyKubeServiceName:   "service-updated",
								constants.MetaKeyKubeNS:  ts.SourceKubeNS,
								metaKeyManagedBy:         constants.ManagedByValue,
								constants.MetaKeyPodName: "pod2",
								metaKeySyntheticNode:     "true",
								constants.MetaKeyPodUID:  "",
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
				node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
				// Create fake k8s client.
				k8sObjects := append(tt.k8sObjects(), &ns, &node)
				fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

				// Create test consulServer server
				adminToken := "123e4567-e89b-12d3-a456-426614174000"
				testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
					if tt.enableACLs {
						c.ACL.Enabled = tt.enableACLs
						c.ACL.Tokens.InitialManagement = adminToken
					}
				})
				consulClient := testClient.APIClient
				// Coincidentally, this allows enough time for the bootstrap token to be generated
				testClient.TestServer.WaitForActiveCARoot(t)

				_, err := namespaces.EnsureExists(consulClient, ts.ExpConsulNS, "")
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
							test.SetupK8sAuthMethodWithNamespaces(t, consulClient, svc.Service.Service, svc.Service.Meta[constants.MetaKeyKubeNS], ts.ExpConsulNS, ts.Mirror, ts.MirrorPrefix, false)
							token, _, err := consulClient.ACL().Login(&api.ACLLoginParams{
								AuthMethod:  test.AuthMethod,
								BearerToken: test.ServiceAccountJWTToken,
								Meta: map[string]string{
									tokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Service.Meta[constants.MetaKeyKubeNS], svc.Service.Meta[constants.MetaKeyPodName]),
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
									tokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Service.Meta[constants.MetaKeyKubeNS], "does-not-exist"),
								},
							}, &writeOpts)
							require.NoError(t, err)
							tokensForServices["does-not-exist"+svc.Service.Service] = token.AccessorID
						}
					}
				}

				// Create the endpoints controller.
				ep := &Controller{
					Client:                     fakeClient,
					Log:                        logrtest.NewTestLogger(t),
					ConsulClientConfig:         testClient.Cfg,
					ConsulServerConnMgr:        testClient.Watcher,
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
				consulConfig := testClient.Cfg
				consulConfig.APIClientConfig.Namespace = ts.ExpConsulNS
				consulClient, err = api.NewClient(consulConfig.APIClientConfig)
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
							require.Contains(t, err.Error(), "ACL not found")
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
// This test covers Controller.deregisterService when the map is nil (not selectively deregistered).
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
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
						Meta:      map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": ts.SourceKubeNS, metaKeyManagedBy: constants.ManagedByValue},
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
							metaKeyKubeServiceName:   "service-deleted",
							constants.MetaKeyKubeNS:  ts.SourceKubeNS,
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "pod1",
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
							metaKeyKubeServiceName:   "service-deleted",
							constants.MetaKeyKubeNS:  ts.SourceKubeNS,
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "pod1",
							metaKeySyntheticNode:     "true",
						},
						Namespace: ts.ExpConsulNS,
					},
				},
				enableACLs: true,
			},
		}
		for _, tt := range cases {
			t.Run(fmt.Sprintf("%s:%s", name, tt.name), func(t *testing.T) {
				node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
				// Create fake k8s client.
				fakeClient := fake.NewClientBuilder().WithRuntimeObjects(&node).Build()

				// Create test consulServer server
				adminToken := "123e4567-e89b-12d3-a456-426614174000"
				testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
					if tt.enableACLs {
						c.ACL.Enabled = tt.enableACLs
						c.ACL.Tokens.InitialManagement = adminToken
					}
				})
				consulClient := testClient.APIClient
				// Coincidentally, this allows enough time for the bootstrap token to be generated
				testClient.TestServer.WaitForActiveCARoot(t)

				_, err := namespaces.EnsureExists(consulClient, ts.ExpConsulNS, "")
				require.NoError(t, err)

				// Register service and proxy in consul.
				var token *api.ACLToken
				for _, svc := range tt.initialConsulSvcs {
					serviceRegistration := &api.CatalogRegistration{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						Service: svc,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
					}
					_, err = consulClient.Catalog().Register(serviceRegistration, nil)
					require.NoError(t, err)
					// Create a token for it if ACLs are enabled.
					if tt.enableACLs {
						var writeOpts api.WriteOptions
						// When mirroring is enabled, the auth method will be created in the "default" Consul namespace.
						if ts.Mirror {
							writeOpts.Namespace = "default"
						} else {
							writeOpts.Namespace = ts.ExpConsulNS
						}
						test.SetupK8sAuthMethodWithNamespaces(t, consulClient, svc.Service, svc.Meta[constants.MetaKeyKubeNS], ts.ExpConsulNS, ts.Mirror, ts.MirrorPrefix, false)
						token, _, err = consulClient.ACL().Login(&api.ACLLoginParams{
							AuthMethod:  test.AuthMethod,
							BearerToken: test.ServiceAccountJWTToken,
							Meta: map[string]string{
								tokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Meta[constants.MetaKeyKubeNS], svc.Meta[constants.MetaKeyPodName]),
							},
						}, &writeOpts)

						require.NoError(t, err)
					}
				}

				// Create the endpoints controller.
				ep := &Controller{
					Client:                     fakeClient,
					Log:                        logrtest.NewTestLogger(t),
					ConsulClientConfig:         testClient.Cfg,
					ConsulServerConnMgr:        testClient.Watcher,
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

				consulConfig := testClient.Cfg
				consulConfig.APIClientConfig.Namespace = ts.ExpConsulNS
				consulClient, err = api.NewClient(consulConfig.APIClientConfig)
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
					require.Contains(t, err.Error(), "ACL not found")
				}
			})
		}
	}
}

// Tests deleting an Endpoints object, with and without matching Consul and K8s service names when Consul namespaces are enabled.
// This test covers Controller.deregisterService when the map is nil (not selectively deregistered).
func TestReconcileDeleteGatewayWithNamespaces(t *testing.T) {
	t.Parallel()

	consulSvcName := "service-deleted"
	cases := map[string]struct {
		ConsulNS string
	}{
		"default Consul namespace": {
			ConsulNS: "default",
		},
		"other Consul namespace": {
			ConsulNS: "other",
		},
	}
	for name, ts := range cases {
		cases := []struct {
			name              string
			initialConsulSvcs []*api.AgentService
			enableACLs        bool
		}{
			{
				name: "mesh-gateway",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:      "mesh-gateway",
						Kind:    api.ServiceKindMeshGateway,
						Service: consulSvcName,
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-deleted",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "mesh-gateway",
							metaKeySyntheticNode:     "true",
						},
						TaggedAddresses: map[string]api.ServiceAddress{
							"lan": {
								Address: "1.2.3.4",
								Port:    80,
							},
							"wan": {
								Address: "5.6.7.8",
								Port:    8080,
							},
						},
						Namespace: "default",
					},
				},
				enableACLs: false,
			},
			{
				name: "mesh-gateway with ACLs enabled",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:      "mesh-gateway",
						Kind:    api.ServiceKindMeshGateway,
						Service: consulSvcName,
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-deleted",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "mesh-gateway",
							metaKeySyntheticNode:     "true",
						},
						TaggedAddresses: map[string]api.ServiceAddress{
							"lan": {
								Address: "1.2.3.4",
								Port:    80,
							},
							"wan": {
								Address: "5.6.7.8",
								Port:    8080,
							},
						},
						Namespace: "default",
					},
				},
				enableACLs: true,
			},
			{
				name: "terminating-gateway",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:      "terminating-gateway",
						Kind:    api.ServiceKindTerminatingGateway,
						Service: consulSvcName,
						Port:    8443,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-deleted",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "terminating-gateway",
							metaKeySyntheticNode:     "true",
						},
						Namespace: ts.ConsulNS,
					},
				},
				enableACLs: false,
			},
			{
				name: "terminating-gateway with ACLs enabled",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:      "terminating-gateway",
						Kind:    api.ServiceKindTerminatingGateway,
						Service: consulSvcName,
						Port:    8443,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-deleted",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "terminating-gateway",
							metaKeySyntheticNode:     "true",
						},
						Namespace: ts.ConsulNS,
					},
				},
				enableACLs: true,
			},
			{
				name: "ingress-gateway",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:      "ingress-gateway",
						Kind:    api.ServiceKindIngressGateway,
						Service: "ingress-gateway",
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "gateway",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "ingress-gateway",
							metaKeySyntheticNode:     "true",
						},
						TaggedAddresses: map[string]api.ServiceAddress{
							"lan": {
								Address: "1.2.3.4",
								Port:    80,
							},
							"wan": {
								Address: "5.6.7.8",
								Port:    8080,
							},
						},
						Namespace: ts.ConsulNS,
					},
				},
				enableACLs: false,
			},
			{
				name: "ingress-gateway with ACLs enabled",
				initialConsulSvcs: []*api.AgentService{
					{
						ID:      "ingress-gateway",
						Kind:    api.ServiceKindIngressGateway,
						Service: consulSvcName,
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-deleted",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							constants.MetaKeyPodName: "ingress-gateway",
							metaKeySyntheticNode:     "true",
						},
						TaggedAddresses: map[string]api.ServiceAddress{
							"lan": {
								Address: "1.2.3.4",
								Port:    80,
							},
							"wan": {
								Address: "5.6.7.8",
								Port:    8080,
							},
						},
						Namespace: ts.ConsulNS,
					},
				},
				enableACLs: true,
			},
		}
		for _, tt := range cases {
			t.Run(fmt.Sprintf("%s:%s", name, tt.name), func(t *testing.T) {
				// Create fake k8s client.
				node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
				fakeClient := fake.NewClientBuilder().WithRuntimeObjects(&node).Build()

				// Create test Consul server.
				adminToken := "123e4567-e89b-12d3-a456-426614174000"
				testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
					if tt.enableACLs {
						c.ACL.Enabled = tt.enableACLs
						c.ACL.Tokens.InitialManagement = adminToken
					}
				})
				consulClient := testClient.APIClient
				// Coincidentally, this allows enough time for the bootstrap token to be generated
				testClient.TestServer.WaitForActiveCARoot(t)

				_, err := namespaces.EnsureExists(consulClient, ts.ConsulNS, "")
				require.NoError(t, err)

				// Register service and proxy in consul.
				var token *api.ACLToken
				for _, svc := range tt.initialConsulSvcs {
					serviceRegistration := &api.CatalogRegistration{
						Node:    consulNodeName,
						Address: consulNodeAddress,
						Service: svc,
						NodeMeta: map[string]string{
							metaKeySyntheticNode: "true",
						},
					}
					_, err = consulClient.Catalog().Register(serviceRegistration, nil)
					require.NoError(t, err)

					// Create a token for it if ACLs are enabled.
					if tt.enableACLs {
						var writeOpts api.WriteOptions
						if svc.Kind == api.ServiceKindMeshGateway {
							writeOpts.Namespace = "default" // Mesh Gateways must always be registered in the "default" namespace.
						} else {
							writeOpts.Namespace = ts.ConsulNS
						}

						test.SetupK8sAuthMethodWithNamespaces(t, consulClient, svc.Service, svc.Meta[constants.MetaKeyKubeNS], writeOpts.Namespace, false, "", false)
						token, _, err = consulClient.ACL().Login(&api.ACLLoginParams{
							AuthMethod:  test.AuthMethod,
							BearerToken: test.ServiceAccountJWTToken,
							Meta: map[string]string{
								tokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Meta[constants.MetaKeyKubeNS], svc.Meta[constants.MetaKeyPodName]),
								"component":         svc.ID,
							},
						}, &writeOpts)

						require.NoError(t, err)
					}
				}

				// Create the endpoints controller.
				ep := &Controller{
					Client:                     fakeClient,
					Log:                        logrtest.NewTestLogger(t),
					ConsulClientConfig:         testClient.Cfg,
					ConsulServerConnMgr:        testClient.Watcher,
					AllowK8sNamespacesSet:      mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:       mapset.NewSetWith(),
					ReleaseName:                "consul",
					ReleaseNamespace:           "default",
					EnableConsulNamespaces:     true,
					ConsulDestinationNamespace: ts.ConsulNS,
				}
				if tt.enableACLs {
					ep.AuthMethod = test.AuthMethod
				}

				// Set up the Endpoint that will be reconciled, and reconcile.
				namespacedName := types.NamespacedName{
					Namespace: "default",
					Name:      "service-deleted",
				}
				resp, err := ep.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: namespacedName,
				})
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				// After reconciliation, Consul should not have any instances of service-deleted.
				defaultNS, _, err := consulClient.Catalog().Service(consulSvcName, "", &api.QueryOptions{Namespace: "default"})
				require.NoError(t, err)
				testNS, _, err := consulClient.Catalog().Service(consulSvcName, "", &api.QueryOptions{Namespace: ts.ConsulNS})
				require.NoError(t, err)
				require.Empty(t, append(defaultNS, testNS...))

				if tt.enableACLs {
					queryOpts := &api.QueryOptions{}
					if tt.initialConsulSvcs[0].Kind == api.ServiceKindMeshGateway {
						queryOpts.Namespace = "default" // Mesh Gateways must always be registered in the "default" namespace.
					} else {
						queryOpts.Namespace = ts.ConsulNS
					}

					token, _, err = consulClient.ACL().TokenRead(token.AccessorID, queryOpts)
					require.Error(t, err)
					require.Contains(t, err.Error(), "ACL not found", token)
				}
			})
		}
	}
}

func createPodWithNamespace(name, namespace, ip string, inject bool, managedByEndpointsController bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{},
			Annotations: map[string]string{
				constants.LegacyAnnotationConsulK8sVersion: "1.0.0",
			},
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: consulNodeAddress,
			Phase:  corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
	if inject {
		pod.Labels[constants.KeyInjectStatus] = constants.Injected
		pod.Annotations[constants.KeyInjectStatus] = constants.Injected
	}
	if managedByEndpointsController {
		pod.Labels[constants.KeyManagedBy] = constants.ManagedByValue
	}
	return pod

}

func createGatewayWithNamespace(name, namespace, ip string, annotations map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.KeyManagedBy: constants.ManagedByValue,
			},
			Annotations: annotations,
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: consulNodeAddress,
			Phase:  corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
	return pod
}
