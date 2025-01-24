// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul/api"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestProcessUpstreams(t *testing.T) {
	t.Parallel()

	const podName = "pod1"

	cases := []struct {
		name                    string
		pod                     func() *corev1.Pod
		expected                *pbmesh.Destinations
		expErr                  string
		configEntry             func() api.ConfigEntry
		consulUnavailable       bool
		consulNamespacesEnabled bool
		consulPartitionsEnabled bool
	}{
		{
			name: "labeled annotated destination with svc only",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc:1234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated destination with svc and dc",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.dc1.dc:1234")
				return pod1
			},
			expErr: "destination currently does not support datacenters: myPort.port.upstream1.svc.dc1.dc:1234",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//				Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: constants.GetNormalizedConsulNamespace(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "dc1",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated destination with svc and peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.peer1.peer:1234")
				return pod1
			},
			expErr: "destination currently does not support peers: myPort.port.upstream1.svc.peer1.peer:1234",
			// TODO: uncomment this and remove expErr when peers is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: constants.GetNormalizedConsulNamespace(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated destination with svc, ns, and peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.peer1.peer:1234")
				return pod1
			},
			expErr: "destination currently does not support peers: myPort.port.upstream1.svc.ns1.ns.peer1.peer:1234",
			// TODO: uncomment this and remove expErr when peers is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			// 			    Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "ns1",
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated destination with svc, ns, and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.part1.ap:1234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "part1",
								Namespace: "ns1",
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "labeled annotated destination with svc, ns, and dc",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234")
				return pod1
			},
			expErr: "destination currently does not support datacenters: myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "ns1",
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "dc1",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled multiple annotated destinations",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns:1234, myPort2.port.upstream2.svc:2234, myPort4.port.upstream4.svc.ns1.ns.ap1.ap:4234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: "ns1",
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream2",
						},
						DestinationPort: "myPort2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(2234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "ap1",
								Namespace: "ns1",
							},
							Name: "upstream4",
						},
						DestinationPort: "myPort4",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(4234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "labeled multiple annotated destinations with dcs and peers",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234, myPort2.port.upstream2.svc:2234, myPort3.port.upstream3.svc.ns1.ns:3234, myPort4.port.upstream4.svc.ns1.ns.peer1.peer:4234")
				return pod1
			},
			expErr: "destination currently does not support datacenters: myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "ns1",
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "dc1",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: constants.GetNormalizedConsulNamespace(""),
			//				},
			//				Name: "upstream2",
			//			},
			//			DestinationPort: "myPort2",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(2234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "ns1",
			//				},
			//				Name: "upstream3",
			//			},
			//			DestinationPort: "myPort3",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(3234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "ns1",
			//				},
			//				Name: "upstream4",
			//			},
			//			DestinationPort: "myPort4",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(4234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated destination error: invalid partition/dc/peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.part1.err:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.ns1.ns.part1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination with svc and peer, needs ns before peer if namespaces enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.peer1.peer:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.peer1.peer:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination error: invalid namespace",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.err:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.ns1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination error: invalid number of pieces in the address",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.err:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination error: invalid peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.peer1.err:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.peer1.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination error: invalid number of pieces in the address without namespaces and partitions",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.err:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination error: both peer and partition provided",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.part1.partition.peer1.peer:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.ns1.ns.part1.partition.peer1.peer:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated destination error: both peer and dc provided",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.peer1.peer.dc1.dc:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.ns1.ns.peer1.peer.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination error: both dc and partition provided",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.port.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated destination error: wrong ordering for port and svc with namespace partition enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "upstream1.svc.myPort.port.ns1.ns.part1.partition.dc1.dc:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: upstream1.svc.myPort.port.ns1.ns.part1.partition.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated destination error: wrong ordering for port and svc with namespace partition disabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "upstream1.svc.myPort.port:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: upstream1.svc.myPort.port:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated destination error: incorrect key name namespace partition enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.portage.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.portage.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated destination error: incorrect key name namespace partition disabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.portage.upstream1.svc:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: myPort.portage.upstream1.svc:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled missing port name",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "upstream1.svc:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: upstream1.svc:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled missing port name namespace partition enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "upstream1.svc:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: upstream1.svc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "unlabeled and labeled multiple annotated destinations",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.port.upstream1.svc.ns1.ns:1234, myPort2.upstream2:2234, myPort4.port.upstream4.svc.ns1.ns.ap1.ap:4234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: "ns1",
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream2",
						},
						DestinationPort: "myPort2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(2234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "ap1",
								Namespace: "ns1",
							},
							Name: "upstream4",
						},
						DestinationPort: "myPort4",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(4234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "unlabeled single destination",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.upstream:1234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled single destination with namespace",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.upstream.foo:1234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: "foo",
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled single destination with namespace and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.upstream.foo.bar:1234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "bar",
								Namespace: "foo",
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "unlabeled multiple destinations",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.upstream1:1234, myPort2.upstream2:2234")
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream2",
						},
						DestinationPort: "myPort2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(2234),
								Ip:   ConsulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled multiple destinations with consul namespaces, partitions and datacenters",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.upstream1:1234, myPort2.upstream2.bar:2234, myPort3.upstream3.foo.baz:3234:dc2")
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "remote"
				return pd
			},
			expErr: "destination currently does not support datacenters:  myPort3.upstream3.foo.baz:3234:dc2",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: constants.GetNormalizedConsulNamespace(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "bar",
			//				},
			//				Name: "upstream2",
			//			},
			//			DestinationPort: "myPort2",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(2234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: "baz",
			//					Namespace: "foo",
			//				},
			//				Name: "upstream3",
			//			},
			//			DestinationPort: "myPort3",
			//			Datacenter:      "dc2",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(3234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "unlabeled multiple destinations with consul namespaces and datacenters",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "myPort.upstream1:1234, myPort2.upstream2.bar:2234, myPort3.upstream3.foo:3234:dc2")
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "remote"
				return pd
			},
			expErr: "destination currently does not support datacenters:  myPort3.upstream3.foo:3234:dc2",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: constants.GetNormalizedConsulNamespace(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "bar",
			//				},
			//				Name: "upstream2",
			//			},
			//			DestinationPort: "myPort2",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(2234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "foo",
			//				},
			//				Name: "upstream3",
			//			},
			//			DestinationPort: "myPort3",
			//			Datacenter:      "dc2",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(3234),
			//                  Ip:   ConsulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
		},
		{
			name: "error unlabeled missing port name with namespace and partition disabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "upstream1:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: upstream1:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error unlabeled missing port name with namespace and partition enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "upstream1:1234")
				return pod1
			},
			expErr:                  "destination structured incorrectly: upstream1:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			destinations, err := ProcessPodDestinations(*tt.pod(), tt.consulNamespacesEnabled, tt.consulPartitionsEnabled)
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, destinations)

				if diff := cmp.Diff(tt.expected, destinations, protocmp.Transform()); diff != "" {
					t.Errorf("unexpected difference:\n%v", diff)
				}
			}
		})
	}
}

// createPod creates a multi-port pod as a base for tests.
func createPod(name string, annotation string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	pod.Annotations = map[string]string{
		constants.AnnotationMeshDestinations: annotation,
	}
	return pod
}
