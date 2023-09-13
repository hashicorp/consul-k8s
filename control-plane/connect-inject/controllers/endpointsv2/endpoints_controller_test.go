// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpointsv2

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/go-uuid"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

var (
	appProtocolHttp  = "http"
	appProtocolHttp2 = "http2"
	appProtocolGrpc  = "grpc"
)

type reconcileCase struct {
	name                  string
	svcName               string
	k8sObjects            func() []runtime.Object
	existingResource      *pbresource.Resource
	expectedResource      *pbresource.Resource
	targetConsulNs        string
	targetConsulPartition string
	expErr                string
}

// TODO: Allow/deny namespaces for reconcile tests
// TODO: ConsulDestinationNamespace and EnableNSMirroring +/- prefix

func TestReconcile_CreateService(t *testing.T) {
	t.Parallel()
	cases := []reconcileCase{
		{
			// In this test, we expect the same service registration as the "basic"
			// case, but without any workload selector values due to missing endpoints.
			name:    "Empty endpoints",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{},
						},
					},
				}
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
							{
								Name:       "other",
								Port:       10001,
								TargetPort: intstr.FromString("10001"),
								// no protocol specified
							},
						},
					},
				}
				return []runtime.Object{endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							VirtualPort: 9090,
							TargetPort:  "my-grpc-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_GRPC,
						},
						{
							VirtualPort: 10001,
							TargetPort:  "10001",
							Protocol:    pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads:  &pbcatalog.WorkloadSelector{},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Basic endpoints",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePod("DaemonSet", "service-created-ds", "12345")
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1, pod2),
							Ports: []corev1.EndpointPort{
								{
									Name:        "my-http-port",
									AppProtocol: &appProtocolHttp,
									Port:        2345,
								},
								{
									Name:        "my-grpc-port",
									AppProtocol: &appProtocolGrpc,
									Port:        6789,
								},
								{
									Name: "10001",
									Port: 10001,
								},
							},
						},
					},
				}
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
							{
								Name:       "other",
								Port:       10001,
								TargetPort: intstr.FromString("10001"),
								// no protocol specified
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							VirtualPort: 9090,
							TargetPort:  "my-grpc-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_GRPC,
						},
						{
							VirtualPort: 10001,
							TargetPort:  "10001",
							Protocol:    pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"service-created-rs-abcde"},
						Names:    []string{"service-created-ds-12345"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Unhealthy endpoints should be registered",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-fghij")
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							// Split addresses between ready and not-ready
							Addresses:         addressesForPods(pod1),
							NotReadyAddresses: addressesForPods(pod2),
							Ports: []corev1.EndpointPort{
								{
									Name:        "my-http-port",
									AppProtocol: &appProtocolHttp,
									Port:        2345,
								},
							},
						},
					},
				}
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						// Both replicasets (ready and not ready) should be present
						Prefixes: []string{
							"service-created-rs-abcde",
							"service-created-rs-fghij",
						},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Pods with only some service ports should be registered",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-fghij")
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						// Two separate endpoint subsets w/ each of 2 ports served by a different replicaset
						{
							Addresses: addressesForPods(pod1),
							Ports: []corev1.EndpointPort{
								{
									Name:        "my-http-port",
									AppProtocol: &appProtocolHttp,
									Port:        2345,
								},
							},
						},
						{
							Addresses: addressesForPods(pod2),
							Ports: []corev1.EndpointPort{
								{
									Name:        "my-grpc-port",
									AppProtocol: &appProtocolGrpc,
									Port:        6789,
								},
							},
						},
					},
				}
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							VirtualPort: 9090,
							TargetPort:  "my-grpc-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_GRPC,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						// Both replicasets should be present even though neither serves both ports
						Prefixes: []string{
							"service-created-rs-abcde",
							"service-created-rs-fghij",
						},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runReconcileCase(t, tc)
		})
	}
}

func TestReconcile_UpdateService(t *testing.T) {
	t.Parallel()
	cases := []reconcileCase{
		{
			name:    "Pods changed",
			svcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-klmno")
				pod3 := createServicePod("DaemonSet", "service-created-ds", "12345")
				pod4 := createServicePod("DaemonSet", "service-created-ds", "34567")
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1, pod2, pod3, pod4),
							Ports: []corev1.EndpointPort{
								{
									Name:        "my-http-port",
									AppProtocol: &appProtocolHttp,
									Port:        2345,
								},
							},
						},
					},
				}
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, pod3, pod4, endpoints, service}
			},
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{
							"service-created-rs-abcde", // Retained
							"service-created-rs-fghij", // Removed
						},
						Names: []string{
							"service-created-ds-12345", // Retained
							"service-created-ds-23456", // Removed
						},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{

						Prefixes: []string{
							"service-created-rs-abcde", // Retained
							"service-created-rs-klmno", // New
						},
						Names: []string{
							"service-created-ds-12345", // Retained
							"service-created-ds-34567", // New
						},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Service ports changed",
			svcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePod("DaemonSet", "service-created-ds", "12345")
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1, pod2),
							Ports: []corev1.EndpointPort{
								{
									Name:        "my-http-port",
									AppProtocol: &appProtocolHttp,
									Port:        2345,
								},
								{
									Name:        "my-grpc-port",
									AppProtocol: &appProtocolHttp,
									Port:        6789,
								},
							},
						},
					},
				}
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								TargetPort:  intstr.FromString("new-http-port"),
								AppProtocol: &appProtocolHttp2,
							},
							{
								Name:        "api",
								Port:        9091,
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints, service}
			},
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-updated",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							VirtualPort: 9090,
							TargetPort:  "my-grpc-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_GRPC,
						},
						{
							VirtualPort: 10001,
							TargetPort:  "10001",
							Protocol:    pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"service-created-rs-abcde"},
						Names:    []string{"service-created-ds-12345"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-updated",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "new-http-port",                   // Updated
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP2, // Updated
						},
						{
							VirtualPort: 9091, // Updated
							TargetPort:  "my-grpc-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_GRPC,
						},
						// Port 10001 removed
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"service-created-rs-abcde"},
						Names:    []string{"service-created-ds-12345"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runReconcileCase(t, tc)
		})
	}
}

func TestReconcile_DeleteService(t *testing.T) {
	t.Parallel()
	cases := []reconcileCase{
		{
			name:    "Basic Endpoints not found (service deleted)",
			svcName: "service-deleted",
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: &pbresource.Type{
						Group:        "catalog",
						GroupVersion: "v1alpha1",
						Kind:         "Service",
					},
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: common.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-http-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"service-created-rs-abcde"},
						Names:    []string{"service-created-ds-12345"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS: constants.DefaultConsulNS,
					metaKeyManagedBy:        constants.ManagedByEndpointsValue,
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runReconcileCase(t, tc)
		})
	}
}

func TestGetWorkloadSelectorFromEndpoints(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	type testCase struct {
		name      string
		endpoints *corev1.Endpoints
		responses map[types.NamespacedName]*corev1.Pod
		expected  *pbcatalog.WorkloadSelector
		mockFn    func(*testing.T, *MockPodFetcher)
	}

	rsPods := []*corev1.Pod{
		createServicePod(kindReplicaSet, "svc-rs-abcde", "12345"),
		createServicePod(kindReplicaSet, "svc-rs-abcde", "23456"),
		createServicePod(kindReplicaSet, "svc-rs-abcde", "34567"),
		createServicePod(kindReplicaSet, "svc-rs-fghij", "12345"),
		createServicePod(kindReplicaSet, "svc-rs-fghij", "23456"),
		createServicePod(kindReplicaSet, "svc-rs-fghij", "34567"),
	}
	otherPods := []*corev1.Pod{
		createServicePod("DaemonSet", "svc-ds", "12345"),
		createServicePod("DaemonSet", "svc-ds", "23456"),
		createServicePod("DaemonSet", "svc-ds", "34567"),
		createServicePod("StatefulSet", "svc-ss", "12345"),
		createServicePod("StatefulSet", "svc-ss", "23456"),
		createServicePod("StatefulSet", "svc-ss", "34567"),
	}
	podsByName := make(map[types.NamespacedName]*corev1.Pod)
	for _, p := range rsPods {
		podsByName[types.NamespacedName{Name: p.Name, Namespace: p.Namespace}] = p
	}
	for _, p := range otherPods {
		podsByName[types.NamespacedName{Name: p.Name, Namespace: p.Namespace}] = p
	}

	cases := []testCase{
		{
			name: "Pod is fetched once per ReplicaSet",
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: addressesForPods(rsPods...),
						Ports: []corev1.EndpointPort{
							{
								Name:        "my-http-port",
								AppProtocol: &appProtocolHttp,
								Port:        2345,
							},
						},
					},
				},
			},
			responses: podsByName,
			expected: getWorkloadSelector(
				// Selector should consist of prefixes only.
				map[string]any{
					"svc-rs-abcde": true,
					"svc-rs-fghij": true,
				},
				map[string]any{}),
			mockFn: func(t *testing.T, pf *MockPodFetcher) {
				// Assert called once per set.
				require.Equal(t, 2, len(pf.calls))
			},
		},
		{
			name: "Pod is fetched once per other pod owner type",
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: addressesForPods(otherPods...),
						Ports: []corev1.EndpointPort{
							{
								Name:        "my-http-port",
								AppProtocol: &appProtocolHttp,
								Port:        2345,
							},
						},
					},
				},
			},
			responses: podsByName,
			expected: getWorkloadSelector(
				// Selector should consist of exact name matches only.
				map[string]any{},
				map[string]any{
					"svc-ds-12345": true,
					"svc-ds-23456": true,
					"svc-ds-34567": true,
					"svc-ss-12345": true,
					"svc-ss-23456": true,
					"svc-ss-34567": true,
				}),
			mockFn: func(t *testing.T, pf *MockPodFetcher) {
				// Assert called once per pod.
				require.Equal(t, len(otherPods), len(pf.calls))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock pod fetcher.
			pf := MockPodFetcher{responses: tc.responses}

			// Create the Endpoints controller.
			ep := &Controller{
				Log: logrtest.New(t),
			}

			resp, err := ep.getWorkloadSelectorFromEndpoints(ctx, &pf, tc.endpoints)
			require.NoError(t, err)

			// We don't care about order, so configure cmp.Diff to ignore slice order.
			sorter := func(a, b string) bool { return a < b }
			if diff := cmp.Diff(tc.expected, resp, protocmp.Transform(), cmpopts.SortSlices(sorter)); diff != "" {
				t.Errorf("unexpected difference:\n%v", diff)
			}
			tc.mockFn(t, &pf)
		})
	}
}

type MockPodFetcher struct {
	calls     []types.NamespacedName
	responses map[types.NamespacedName]*corev1.Pod
}

func (m *MockPodFetcher) GetPod(_ context.Context, name types.NamespacedName) (*corev1.Pod, error) {
	m.calls = append(m.calls, name)
	if v, ok := m.responses[name]; !ok {
		panic(fmt.Errorf("test is missing response for passed pod name: %v", name))
	} else {
		return v, nil
	}
}

func runReconcileCase(t *testing.T, tc reconcileCase) {
	t.Helper()

	// Create fake k8s client
	var k8sObjects []runtime.Object
	if tc.k8sObjects != nil {
		k8sObjects = tc.k8sObjects()
	}
	fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

	// Create test Consul server.
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})

	// Create the Endpoints controller.
	ep := &Controller{
		Client:                fakeClient,
		Log:                   logrtest.New(t),
		ConsulServerConnMgr:   testClient.Watcher,
		AllowK8sNamespacesSet: mapset.NewSetWith("*"),
		DenyK8sNamespacesSet:  mapset.NewSetWith(),
	}
	resourceClient, err := consul.NewResourceServiceClient(ep.ConsulServerConnMgr)
	require.NoError(t, err)

	// Default ns and partition if not specified in test.
	if tc.targetConsulNs == "" {
		tc.targetConsulNs = constants.DefaultConsulNS
	}
	if tc.targetConsulPartition == "" {
		tc.targetConsulPartition = constants.DefaultConsulPartition
	}

	// If existing resource specified, create it and ensure it exists.
	if tc.existingResource != nil {
		writeReq := &pbresource.WriteRequest{Resource: tc.existingResource}
		_, err = resourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)
		test.ResourceHasPersisted(t, resourceClient, tc.existingResource.Id)
	}

	// Run actual reconcile and verify results.
	resp, err := ep.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      tc.svcName,
			Namespace: tc.targetConsulNs,
		},
	})
	if tc.expErr != "" {
		require.ErrorContains(t, err, tc.expErr)
	} else {
		require.NoError(t, err)
	}
	require.False(t, resp.Requeue)

	expectedServiceMatches(t, resourceClient, tc.svcName, tc.targetConsulNs, tc.targetConsulPartition, tc.expectedResource)

}

func expectedServiceMatches(t *testing.T, client pbresource.ResourceServiceClient, name, namespace, partition string, expectedResource *pbresource.Resource) {
	req := &pbresource.ReadRequest{Id: getServiceID(name, namespace, partition)}

	res, err := client.Read(context.Background(), req)

	if expectedResource == nil {
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
		return
	}

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.GetResource().GetData())

	expectedService := &pbcatalog.Service{}
	err = anypb.UnmarshalTo(expectedResource.Data, expectedService, proto.UnmarshalOptions{})
	require.NoError(t, err)

	actualService := &pbcatalog.Service{}
	err = res.GetResource().GetData().UnmarshalTo(actualService)
	require.NoError(t, err)

	if diff := cmp.Diff(expectedService, actualService, protocmp.Transform()); diff != "" {
		t.Errorf("unexpected difference:\n%v", diff)
	}
}

func createServicePodOwnedBy(ownerKind, ownerName string) *corev1.Pod {
	return createServicePod(ownerKind, ownerName, randomKubernetesId())
}

func createServicePod(ownerKind, ownerName, podId string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", ownerName, podId),
			Namespace: "default",
			Labels:    map[string]string{},
			Annotations: map[string]string{
				constants.AnnotationConsulK8sVersion: "1.3.0",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: ownerName,
					Kind: ownerKind,
				},
			},
		},
	}
	return pod
}

func addressesForPods(pods ...*corev1.Pod) []corev1.EndpointAddress {
	var addresses []corev1.EndpointAddress
	for i, p := range pods {
		addresses = append(addresses, corev1.EndpointAddress{
			IP: fmt.Sprintf("1.2.3.%d", i),
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Name:      p.Name,
				Namespace: p.Namespace,
			},
		})
	}
	return addresses
}

func randomKubernetesId() string {
	u, err := uuid.GenerateUUID()
	if err != nil {
		panic(err)
	}
	return u[:5]
}
