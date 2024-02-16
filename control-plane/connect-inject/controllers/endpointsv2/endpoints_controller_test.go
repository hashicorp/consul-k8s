// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpointsv2

import (
	"context"
	"fmt"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/go-uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	kindDaemonSet = "DaemonSet"
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
	caseFn                func(*testing.T, *reconcileCase, *Controller, pbresource.ResourceServiceClient)
}

// TODO(NET-5716): Allow/deny namespaces for reconcile tests

func TestReconcile_CreateService(t *testing.T) {
	t.Parallel()
	cases := []reconcileCase{
		{
			name:    "Empty endpoints do not get registered",
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
							{
								Name:       "other",
								Port:       10001,
								Protocol:   "TCP",
								TargetPort: intstr.FromString("10001"),
								// no app protocol specified
							},
						},
					},
				}
				return []runtime.Object{endpoints, service}
			},
		},
		{
			name:    "Endpoints without injected pods do not get registered",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePod(kindDaemonSet, "service-created-ds", "12345")
				removeMeshInjectStatus(t, pod1)
				removeMeshInjectStatus(t, pod2)
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1, pod2),
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
							{
								Name:       "other",
								Port:       10001,
								Protocol:   "TCP",
								TargetPort: intstr.FromString("10001"),
								// no app protocol specified
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints, service}
			},
		},
		{
			name:    "Basic endpoints",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePod(kindDaemonSet, "service-created-ds", "12345")
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
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
								},
								{
									Name:        "api",
									Port:        6789,
									Protocol:    "TCP",
									AppProtocol: &appProtocolGrpc,
								},
								{
									Name:     "other",
									Port:     10001,
									Protocol: "TCP",
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
							{
								Name:       "other",
								Port:       10001,
								Protocol:   "TCP",
								TargetPort: intstr.FromString("cslport-10001"),
								// no app protocol specified
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
							TargetPort:  "cslport-10001",
							Protocol:    pbcatalog.Protocol_PROTOCOL_TCP,
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
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
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
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
								Protocol:    "TCP",
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
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
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
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
								},
							},
						},
						{
							Addresses: addressesForPods(pod2),
							Ports: []corev1.EndpointPort{
								{
									Name:        "api",
									Port:        6789,
									Protocol:    "TCP",
									AppProtocol: &appProtocolGrpc,
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								Protocol:    "TCP",
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
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Numeric service target port: Named container port gets the pod port name",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde",
					// Named port with container port value matching service target port
					containerWithPort("named-port", 2345),
					// Unnamed port with container port value matching service target port
					containerWithPort("", 6789))
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1),
							Ports: []corev1.EndpointPort{
								{
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
								},
								{
									Name:        "api",
									Port:        6789,
									Protocol:    "TCP",
									AppProtocol: &appProtocolGrpc,
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromInt(2345), // Numeric target port
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								Protocol:    "TCP",
								TargetPort:  intstr.FromInt(6789), // Numeric target port
								AppProtocol: &appProtocolGrpc,
							},
							{
								Name:        "unmatched-port",
								Port:        10010,
								Protocol:    "TCP",
								TargetPort:  intstr.FromInt(10010), // Numeric target port
								AppProtocol: &appProtocolHttp,
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "named-port", // Matches container port name, not service target number
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							VirtualPort: 9090,
							TargetPort:  "cslport-6789", // Matches service target number
							Protocol:    pbcatalog.Protocol_PROTOCOL_GRPC,
						},
						{
							VirtualPort: 10010,
							TargetPort:  "cslport-10010", // Matches service target number (unmatched by container ports)
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"service-created-rs-abcde"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Numeric service target port: Container port mix gets the name from largest matching pod set",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				// Unnamed port matching service target port.
				// Also has second named port, and is not the most prevalent set for that port.
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde",
					containerWithPort("", 2345),
					containerWithPort("api-port", 6789))

				// Named port with different name from most prevalent pods.
				// Also has second unnamed port, and _is_ the most prevalent set for that port.
				pod2a := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-fghij",
					containerWithPort("another-port-name", 2345),
					containerWithPort("", 6789))
				pod2b := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-fghij",
					containerWithPort("another-port-name", 2345),
					containerWithPort("", 6789))

				// Named port with container port value matching service target port.
				// The most common "set" of pods, so should become the port name for service target port.
				pod3a := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-klmno",
					containerWithPort("named-port", 2345))
				pod3b := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-klmno",
					containerWithPort("named-port", 2345))
				pod3c := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-klmno",
					containerWithPort("named-port", 2345))

				// Named port that does not match service target port.
				// More common "set" of pods selected by the service, but does not have a target port (value) match.
				pod4a := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-pqrst",
					containerWithPort("non-matching-named-port", 5432))
				pod4b := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-pqrst",
					containerWithPort("non-matching-named-port", 5432))
				pod4c := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-pqrst",
					containerWithPort("non-matching-named-port", 5432))
				pod4d := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-pqrst",
					containerWithPort("non-matching-named-port", 5432))

				// Named port from non-injected pods.
				// More common "set" of pods selected by the service, but should be filtered out.
				pod5a := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-uvwxy",
					containerWithPort("ignored-named-port", 2345))
				pod5b := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-uvwxy",
					containerWithPort("ignored-named-port", 2345))
				pod5c := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-uvwxy",
					containerWithPort("ignored-named-port", 2345))
				pod5d := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-uvwxy",
					containerWithPort("ignored-named-port", 2345))
				for _, p := range []*corev1.Pod{pod5a, pod5b, pod5c, pod5d} {
					removeMeshInjectStatus(t, p)
				}

				// Named port with container port value matching service target port.
				// Single pod from non-ReplicaSet owner. Should not take precedence over set pods.
				pod6a := createServicePod(kindDaemonSet, "service-created-ds", "12345",
					containerWithPort("another-port-name", 2345))

				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(
								pod1,
								pod2a, pod2b,
								pod3a, pod3b, pod3c,
								pod4a, pod4b, pod4c, pod4d,
								pod5a, pod5b, pod5c, pod5d,
								pod6a),
							Ports: []corev1.EndpointPort{
								{
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromInt(2345), // Numeric target port
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								Protocol:    "TCP",
								TargetPort:  intstr.FromInt(6789), // Numeric target port
								AppProtocol: &appProtocolGrpc,
							},
						},
					},
				}
				return []runtime.Object{
					pod1,
					pod2a, pod2b,
					pod3a, pod3b, pod3c,
					pod4a, pod4b, pod4c, pod4d,
					pod5a, pod5b, pod5c, pod5d,
					pod6a,
					endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "named-port", // Matches container port name, not service target number
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							VirtualPort: 9090,
							TargetPort:  "cslport-6789", // Matches service target number due to unnamed being most common
							Protocol:    pbcatalog.Protocol_PROTOCOL_GRPC,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{
							"service-created-rs-abcde",
							"service-created-rs-fghij",
							"service-created-rs-klmno",
							"service-created-rs-pqrst",
						},
						Names: []string{
							"service-created-ds-12345",
						},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Numeric service target port: Most used container port name from exact name pods used when no pod sets present",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				// Named port with different name from most prevalent pods.
				pod1a := createServicePod(kindDaemonSet, "service-created-ds1", "12345",
					containerWithPort("another-port-name", 2345))

				// Named port with container port value matching service target port.
				// The most common container port name, so should become the port name for service target port.
				pod2a := createServicePod(kindDaemonSet, "service-created-ds2", "12345",
					containerWithPort("named-port", 2345))
				pod2b := createServicePod(kindDaemonSet, "service-created-ds2", "23456",
					containerWithPort("named-port", 2345))

				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(
								pod1a,
								pod2a, pod2b),
							Ports: []corev1.EndpointPort{
								{
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromInt(2345), // Numeric target port
								AppProtocol: &appProtocolHttp,
							},
						},
					},
				}
				return []runtime.Object{
					pod1a,
					pod2a, pod2b,
					endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "named-port", // Matches container port name, not service target number
							Protocol:    pbcatalog.Protocol_PROTOCOL_HTTP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Names: []string{
							"service-created-ds1-12345",
							"service-created-ds2-12345",
							"service-created-ds2-23456",
						},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Only L4 TCP ports get a Consul Service port when L4 protocols are multiplexed",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1),
							Ports: []corev1.EndpointPort{
								{
									Name:     "public-tcp",
									Port:     2345,
									Protocol: "TCP",
								},
								{
									Name:     "public-udp",
									Port:     2345,
									Protocol: "UDP",
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
							// Two L4 protocols on one exposed port
							{
								Name:       "public-tcp",
								Port:       8080,
								Protocol:   "TCP",
								TargetPort: intstr.FromString("my-svc-port"),
							},
							{
								Name:       "public-udp",
								Port:       8080,
								Protocol:   "UDP",
								TargetPort: intstr.FromString("my-svc-port"),
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
					Ports: []*pbcatalog.ServicePort{
						{
							VirtualPort: 8080,
							TargetPort:  "my-svc-port",
							Protocol:    pbcatalog.Protocol_PROTOCOL_TCP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"service-created-rs-abcde"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Services without mesh-injected pods should not be registered",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				removeMeshInjectStatus(t, pod1)
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1),
							Ports: []corev1.EndpointPort{
								{
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoints, service}
			},
			// No expected resource
		},
		{
			name:    "Services with mix of injected and non-injected pods registered with only injected selectors",
			svcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-fghij")
				pod3 := createServicePod(kindDaemonSet, "service-created-ds", "12345")
				pod4 := createServicePod(kindDaemonSet, "service-created-ds", "23456")
				removeMeshInjectStatus(t, pod1)
				removeMeshInjectStatus(t, pod3)
				// Retain status of second pod
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: addressesForPods(pod1, pod2, pod3, pod4),
							Ports: []corev1.EndpointPort{
								{
									Name:        "public",
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, pod3, pod4, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
						// Selector only contains values for injected pods
						Prefixes: []string{"service-created-rs-fghij"},
						Names:    []string{"service-created-ds-23456"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
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
				pod3 := createServicePod(kindDaemonSet, "service-created-ds", "12345")
				pod4 := createServicePod(kindDaemonSet, "service-created-ds", "34567")
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
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
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
								Protocol:    "TCP",
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
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-created",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Service ports changed",
			svcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePod(kindDaemonSet, "service-created-ds", "12345")
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
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
								},
								{
									Name:        "my-grpc-port",
									Port:        6789,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
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
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("new-http-port"),
								AppProtocol: &appProtocolHttp2,
							},
							{
								Name:        "api",
								Port:        9091,
								Protocol:    "TCP",
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
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
							TargetPort:  "unspec-port", //this might need to be changed to "my_unspecified_port"
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-updated",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
		},
		{
			name:    "Redundant reconcile does not write to Consul unless resource was modified",
			svcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
				pod2 := createServicePodOwnedBy(kindReplicaSet, "service-created-rs-abcde")
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
									Port:        2345,
									Protocol:    "TCP",
									AppProtocol: &appProtocolHttp,
								},
								{
									Name:        "my-grpc-port",
									Port:        6789,
									Protocol:    "TCP",
									AppProtocol: &appProtocolGrpc,
								},
								{
									Name:     "other",
									Port:     10001,
									Protocol: "TCP",
								},
							},
						},
					},
				}
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
						UID:       types.UID(randomUid()),
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
							{
								Name:        "api",
								Port:        9090,
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-grpc-port"),
								AppProtocol: &appProtocolGrpc,
							},
							{
								Name:       "other",
								Port:       10001,
								Protocol:   "TCP",
								TargetPort: intstr.FromString("cslport-10001"),
								// no app protocol specified
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpoints, service}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-updated",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
							TargetPort:  "cslport-10001",
							Protocol:    pbcatalog.Protocol_PROTOCOL_TCP,
						},
						{
							TargetPort: "mesh",
							Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
						},
					},
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"service-created-rs-abcde"},
					},
					VirtualIps: []string{"172.18.0.1"},
				}),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
			caseFn: func(t *testing.T, tc *reconcileCase, ep *Controller, resourceClient pbresource.ResourceServiceClient) {
				runReconcile := func() {
					r, err := ep.Reconcile(context.Background(), ctrl.Request{
						NamespacedName: types.NamespacedName{
							Name:      tc.svcName,
							Namespace: tc.targetConsulNs,
						}})
					require.False(t, r.Requeue)
					require.NoError(t, err)
				}

				// Get resource before additional reconcile
				beforeResource := getAndValidateResource(t, resourceClient, tc.expectedResource.Id)

				// Run several additional reconciles, expecting no writes to Consul
				for i := 0; i < 5; i++ {
					runReconcile()
					require.Equal(t, beforeResource.GetGeneration(),
						getAndValidateResource(t, resourceClient, tc.expectedResource.Id).GetGeneration(),
						"wanted same version for before and after resources following repeat reconcile")
				}

				// Modify resource external to controller
				modified := proto.Clone(beforeResource).(*pbresource.Resource)
				modified.Metadata = map[string]string{"foo": "bar"}
				modified.Version = ""
				modified.Generation = ""
				_, err := resourceClient.Write(context.Background(), &pbresource.WriteRequest{
					Resource: modified,
				})
				require.NoError(t, err)

				// Get resource after additional reconcile, now expecting a new write to occur
				runReconcile()

				require.NotEqual(t, beforeResource.GetGeneration(),
					getAndValidateResource(t, resourceClient, tc.expectedResource.Id).GetGeneration(),
					"wanted different version for before and after resources following modification and reconcile")

				// Get resource before additional reconcile
				beforeResource = getAndValidateResource(t, resourceClient, tc.expectedResource.Id)

				// Run several additional reconciles, expecting no writes to Consul
				for i := 0; i < 5; i++ {
					runReconcile()
					require.Equal(t, beforeResource.GetGeneration(),
						getAndValidateResource(t, resourceClient, tc.expectedResource.Id).GetGeneration(),
						"wanted same version for before and after resources following repeat reconcile")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runReconcileCase(t, tc)
		})
	}
}

func TestEnsureService(t *testing.T) {
	t.Parallel()

	type args struct {
		k8sUid    string
		meta      map[string]string
		consulSvc *pbcatalog.Service
	}

	uuid1 := randomUid()
	uuid2 := randomUid()
	meta1 := getServiceMeta(corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		}})
	meta2 := getServiceMeta(corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		}})
	meta2["some-other-key"] = "value"

	id := getServiceID(
		"my-svc",
		constants.DefaultConsulNS,
		constants.DefaultConsulPartition)

	cases := []struct {
		name              string
		beforeArgs        args
		afterArgs         args
		readFn            func(context.Context, *pbresource.ReadRequest) (*pbresource.ReadResponse, error)
		writeFn           func(context.Context, *pbresource.WriteRequest) (*pbresource.WriteResponse, error)
		expectWrite       bool
		expectAlwaysWrite bool
		expectErr         string
		caseFn            func(t *testing.T, ep *Controller)
	}{
		{
			name: "Identical args writes once",
			beforeArgs: args{
				k8sUid: uuid1,
				meta:   meta1,
				consulSvc: &pbcatalog.Service{
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
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			// Identical to before
			afterArgs: args{
				k8sUid: uuid1,
				meta:   meta1,
				consulSvc: &pbcatalog.Service{
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
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			expectWrite: false,
		},
		{
			name: "Changed service payload updates resource",
			beforeArgs: args{
				k8sUid: uuid1,
				meta:   meta1,
				consulSvc: &pbcatalog.Service{
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
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			afterArgs: args{
				k8sUid: uuid1,
				meta:   meta1,
				consulSvc: &pbcatalog.Service{
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
						// Different workload selector
						Prefixes: []string{"service-created-rs-fghij"},
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			expectWrite: true,
		},
		{
			name: "Changed service meta updates resource",
			beforeArgs: args{
				k8sUid: uuid1,
				meta:   meta1,
				consulSvc: &pbcatalog.Service{
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
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			afterArgs: args{
				k8sUid: uuid1,
				meta:   meta2, // Updated meta
				consulSvc: &pbcatalog.Service{
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
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			expectWrite: true,
		},
		{
			name: "Changed k8s UID updates resource",
			beforeArgs: args{
				k8sUid: uuid1,
				consulSvc: &pbcatalog.Service{
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
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			// Identical to before except K8s UID, indicating delete and rewrite of K8s service
			afterArgs: args{
				k8sUid: uuid2,
				consulSvc: &pbcatalog.Service{
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
					},
					VirtualIps: []string{"172.18.0.1"},
				},
			},
			expectWrite: true,
		},
		{
			name: "Read not found fails open and writes update",
			readFn: func(context.Context, *pbresource.ReadRequest) (*pbresource.ReadResponse, error) {
				return nil, status.Error(codes.NotFound, "not found")
			},
			expectWrite:       true,
			expectAlwaysWrite: true,
		},
		{
			name: "Read error fails open and writes update",
			readFn: func(context.Context, *pbresource.ReadRequest) (*pbresource.ReadResponse, error) {
				return nil, status.Error(codes.PermissionDenied, "not allowed")
			},
			expectWrite:       true,
			expectAlwaysWrite: true,
		},
		{
			name: "Write error does not prevent future writes (cache not updated)",
			writeFn: func(ctx context.Context, request *pbresource.WriteRequest) (*pbresource.WriteResponse, error) {
				return nil, status.Error(codes.Internal, "oops")
			},
			expectErr: "rpc error: code = Internal desc = oops",
			caseFn: func(t *testing.T, ep *Controller) {
				require.Empty(t, ep.WriteCache.(*writeCache).data)
			},
		},
	}

	// Create test Consul server.
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Create the Endpoints controller.
			ep := &Controller{
				Client:              fake.NewClientBuilder().WithRuntimeObjects().Build(), // No k8s fetches should be needed
				WriteCache:          NewWriteCache(logrtest.New(t)),
				Log:                 logrtest.New(t),
				ConsulServerConnMgr: testClient.Watcher,
				K8sNamespaceConfig: common.K8sNamespaceConfig{
					AllowK8sNamespacesSet: mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:  mapset.NewSetWith(),
				},
			}

			// Set up test resourceReadWriter
			rw := struct{ testReadWriter }{}
			defaultRw := defaultResourceReadWriter{testClient.ResourceClient}
			rw.readFn = defaultRw.Read
			rw.writeFn = defaultRw.Write
			if tc.readFn != nil {
				rw.readFn = tc.readFn
			}
			if tc.writeFn != nil {
				rw.writeFn = tc.writeFn
			}

			// Ensure caseFn runs if provided, regardless of whether error is expected
			if tc.caseFn != nil {
				defer tc.caseFn(t, ep)
			}

			// Call first time
			err := ep.ensureService(context.Background(), &rw, tc.beforeArgs.k8sUid, id, tc.beforeArgs.meta, tc.beforeArgs.consulSvc)
			if tc.expectErr != "" {
				require.Contains(t, err.Error(), tc.expectErr)
				return
			}
			require.NoError(t, err)

			// Get written resource before additional calls
			beforeResource := getAndValidateResource(t, testClient.ResourceClient, id)

			// Call a second time
			err = ep.ensureService(context.Background(), &rw, tc.afterArgs.k8sUid, id, tc.afterArgs.meta, tc.afterArgs.consulSvc)
			require.NoError(t, err)

			// Check for change on second call to ensureService
			if tc.expectWrite {
				require.NotEqual(t, beforeResource.GetGeneration(), getAndValidateResource(t, testClient.ResourceClient, id).GetGeneration(),
					"wanted different version for before and after resources following modification and reconcile")
			} else {
				require.Equal(t, beforeResource.GetGeneration(), getAndValidateResource(t, testClient.ResourceClient, id).GetGeneration(),
					"wanted same version for before and after resources following repeat reconcile")
			}

			// Call several additional times
			for i := 0; i < 5; i++ {
				// Get written resource before each additional call
				beforeResource = getAndValidateResource(t, testClient.ResourceClient, id)

				err := ep.ensureService(context.Background(), &rw, tc.afterArgs.k8sUid, id, tc.afterArgs.meta, tc.afterArgs.consulSvc)
				require.NoError(t, err)

				if tc.expectAlwaysWrite {
					require.NotEqual(t, beforeResource.GetGeneration(), getAndValidateResource(t, testClient.ResourceClient, id).GetGeneration(),
						"wanted different version for before and after resources following modification and reconcile")
				} else {
					require.Equal(t, beforeResource.GetGeneration(), getAndValidateResource(t, testClient.ResourceClient, id).GetGeneration(),
						"wanted same version for before and after resources following repeat reconcile")
				}
			}
		})
	}
}

type testReadWriter struct {
	readFn  func(context.Context, *pbresource.ReadRequest) (*pbresource.ReadResponse, error)
	writeFn func(context.Context, *pbresource.WriteRequest) (*pbresource.WriteResponse, error)
}

func (rw *testReadWriter) Read(ctx context.Context, req *pbresource.ReadRequest) (*pbresource.ReadResponse, error) {
	return rw.readFn(ctx, req)
}

func (rw *testReadWriter) Write(ctx context.Context, req *pbresource.WriteRequest) (*pbresource.WriteResponse, error) {
	return rw.writeFn(ctx, req)
}

func TestReconcile_DeleteService(t *testing.T) {
	t.Parallel()
	cases := []reconcileCase{
		{
			name:    "Basic Endpoints not found (service deleted) deregisters service",
			svcName: "service-deleted",
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-deleted",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
			caseFn: func(t *testing.T, _ *reconcileCase, ep *Controller, _ pbresource.ResourceServiceClient) {
				// Ensure cache was also cleared
				require.Empty(t, ep.WriteCache.(*writeCache).data)
			},
		},
		{
			name:    "Empty endpoints does not cause deregistration of existing service",
			svcName: "service-deleted",
			k8sObjects: func() []runtime.Object {
				endpoints := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-deleted",
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
						Name:      "service-deleted",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.18.0.1",
						Ports: []corev1.ServicePort{
							{
								Name:        "public",
								Port:        8080,
								Protocol:    "TCP",
								TargetPort:  intstr.FromString("my-http-port"),
								AppProtocol: &appProtocolHttp,
							},
						},
					},
				}
				return []runtime.Object{endpoints, service}
			},
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-deleted",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
				},
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "service-deleted",
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: inject.ToProtoAny(&pbcatalog.Service{
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
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
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
		endpoints corev1.Endpoints
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
		createServicePod(kindDaemonSet, "svc-ds", "12345"),
		createServicePod(kindDaemonSet, "svc-ds", "23456"),
		createServicePod(kindDaemonSet, "svc-ds", "34567"),
		createServicePod("StatefulSet", "svc-ss", "12345"),
		createServicePod("StatefulSet", "svc-ss", "23456"),
		createServicePod("StatefulSet", "svc-ss", "34567"),
	}
	ignoredPods := []*corev1.Pod{
		createServicePod(kindReplicaSet, "svc-rs-ignored-klmno", "12345"),
		createServicePod(kindReplicaSet, "svc-rs-ignored-klmno", "23456"),
		createServicePod(kindReplicaSet, "svc-rs-ignored-klmno", "34567"),
	}

	podsByName := make(map[types.NamespacedName]*corev1.Pod)
	for _, p := range rsPods {
		podsByName[types.NamespacedName{Name: p.Name, Namespace: p.Namespace}] = p
	}
	for _, p := range otherPods {
		podsByName[types.NamespacedName{Name: p.Name, Namespace: p.Namespace}] = p
	}
	for _, p := range ignoredPods {
		removeMeshInjectStatus(t, p)
		podsByName[types.NamespacedName{Name: p.Name, Namespace: p.Namespace}] = p
	}

	cases := []testCase{
		{
			name: "Pod is fetched once per ReplicaSet",
			endpoints: corev1.Endpoints{
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
				selectorPodData{
					"svc-rs-abcde": &podSetData{},
					"svc-rs-fghij": &podSetData{},
				},
				selectorPodData{}),
			mockFn: func(t *testing.T, pf *MockPodFetcher) {
				// Assert called once per set.
				require.Equal(t, 2, len(pf.calls))
			},
		},
		{
			name: "Pod is fetched once per other pod owner type",
			endpoints: corev1.Endpoints{
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
				selectorPodData{},
				selectorPodData{
					"svc-ds-12345": &podSetData{},
					"svc-ds-23456": &podSetData{},
					"svc-ds-34567": &podSetData{},
					"svc-ss-12345": &podSetData{},
					"svc-ss-23456": &podSetData{},
					"svc-ss-34567": &podSetData{},
				}),
			mockFn: func(t *testing.T, pf *MockPodFetcher) {
				// Assert called once per pod.
				require.Equal(t, len(otherPods), len(pf.calls))
			},
		},
		{
			name: "Pod is ignored if not mesh-injected",
			endpoints: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: addressesForPods(ignoredPods...),
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
			expected:  nil,
			mockFn: func(t *testing.T, pf *MockPodFetcher) {
				// Assert called once for single set.
				require.Equal(t, 1, len(pf.calls))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock pod fetcher.
			pf := MockPodFetcher{responses: tc.responses}

			// Create the Endpoints controller.
			ep := &Controller{
				WriteCache: NewWriteCache(logrtest.New(t)),
				Log:        logrtest.New(t),
			}

			prefixedPods, exactNamePods, err := ep.getWorkloadDataFromEndpoints(ctx, &pf, tc.endpoints)
			require.NoError(t, err)

			ws := getWorkloadSelector(prefixedPods, exactNamePods)
			if diff := cmp.Diff(tc.expected, ws, test.CmpProtoIgnoreOrder()...); diff != "" {
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
		Client:              fakeClient,
		WriteCache:          NewWriteCache(logrtest.New(t)),
		Log:                 logrtest.New(t),
		ConsulServerConnMgr: testClient.Watcher,
		K8sNamespaceConfig: common.K8sNamespaceConfig{
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
		},
	}

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
		_, err := testClient.ResourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)
		test.ResourceHasPersisted(t, context.Background(), testClient.ResourceClient, tc.existingResource.Id)
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

	expectedServiceMatches(t, testClient.ResourceClient, tc.svcName, tc.targetConsulNs, tc.targetConsulPartition, tc.expectedResource)

	if tc.caseFn != nil {
		tc.caseFn(t, &tc, ep, testClient.ResourceClient)
	}
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

	if diff := cmp.Diff(expectedService, actualService, test.CmpProtoIgnoreOrder()...); diff != "" {
		t.Errorf("unexpected difference:\n%v", diff)
	}
}

func createServicePodOwnedBy(ownerKind, ownerName string, containers ...corev1.Container) *corev1.Pod {
	return createServicePod(ownerKind, ownerName, randomKubernetesId(), containers...)
}

func createServicePod(ownerKind, ownerName, podId string, containers ...corev1.Container) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", ownerName, podId),
			Namespace: "default",
			Labels:    map[string]string{},
			Annotations: map[string]string{
				constants.AnnotationConsulK8sVersion: "1.3.0",
				constants.KeyMeshInjectStatus:        constants.Injected,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: ownerName,
					Kind: ownerKind,
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: containers,
		},
	}
	return pod
}

func containerWithPort(name string, port int32) corev1.Container {
	return corev1.Container{
		Ports: []corev1.ContainerPort{
			{
				Name:          name,
				ContainerPort: port,
				Protocol:      "TCP",
			},
		},
	}
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

func randomUid() string {
	u, err := uuid.GenerateUUID()
	if err != nil {
		panic(err)
	}
	return u
}

func removeMeshInjectStatus(t *testing.T, pod *corev1.Pod) {
	delete(pod.Annotations, constants.KeyMeshInjectStatus)
	require.False(t, inject.HasBeenMeshInjected(*pod))
}

func getAndValidateResource(t *testing.T, resourceClient pbresource.ResourceServiceClient, id *pbresource.ID) *pbresource.Resource {
	resp, err := resourceClient.Read(metadata.NewOutgoingContext(
		context.Background(),
		// Read with strong consistency to avoid race conditions
		metadata.New(map[string]string{"x-consul-consistency-mode": "consistent"}),
	), &pbresource.ReadRequest{
		Id: id,
	})
	require.NoError(t, err)
	r := resp.GetResource()
	require.NotNil(t, r)
	require.NotEmpty(t, r.GetGeneration())
	return r
}
