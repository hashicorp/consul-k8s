// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"
	"strings"

	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	ConsulNodeAddress = "127.0.0.1"
)

// ProcessPodDestinationsForMeshWebhook reads the list of destinations from the Pod annotation and converts them into a pbmesh.Destinations
// object.
func ProcessPodDestinationsForMeshWebhook(pod corev1.Pod) (*pbmesh.Destinations, error) {
	return ProcessPodDestinations(pod, true, true)
}

// ProcessPodDestinations reads the list of destinations from the Pod annotation and converts them into a pbmesh.Destinations
// object.
func ProcessPodDestinations(pod corev1.Pod, enablePartitions, enableNamespaces bool) (*pbmesh.Destinations, error) {
	destinations := &pbmesh.Destinations{}
	raw, ok := pod.Annotations[constants.AnnotationMeshDestinations]
	if !ok || raw == "" {
		return nil, nil
	}

	destinations.Workloads = &pbcatalog.WorkloadSelector{
		Names: []string{pod.Name},
	}

	for _, raw := range strings.Split(raw, ",") {
		var destination *pbmesh.Destination

		// Determine the type of processing required unlabeled or labeled
		// [service-port-name].[service-name].[service-namespace].[service-partition]:[port]:[optional datacenter]
		// or
		// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-peer].peer:[port]
		// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-partition].ap:[port]
		// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-datacenter].dc:[port]

		// Scan the string for the annotation keys.
		// Even if the first key is missing, and the order is unexpected, we should let the processing
		// provide us with errors
		labeledFormat := false
		keys := []string{"port", "svc", "ns", "ap", "peer", "dc"}
		for _, v := range keys {
			if strings.Contains(raw, fmt.Sprintf(".%s.", v)) || strings.Contains(raw, fmt.Sprintf(".%s:", v)) {
				labeledFormat = true
				break
			}
		}

		if labeledFormat {
			var err error
			destination, err = processPodLabeledDestination(pod, raw, enablePartitions, enableNamespaces)
			if err != nil {
				return nil, err
			}
		} else {
			var err error
			destination, err = processPodUnlabeledDestination(pod, raw, enablePartitions, enableNamespaces)
			if err != nil {
				return nil, err
			}
		}

		destinations.Destinations = append(destinations.Destinations, destination)
	}

	return destinations, nil
}

// processPodLabeledDestination processes a destination in the format:
// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-peer].peer:[port]
// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-partition].ap:[port]
// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-datacenter].dc:[port].
// peer/ap/dc are mutually exclusive. At minimum service-port-name and service-name are required.
// The ordering matters for labeled as well as unlabeled. The ordering of the labeled parameters should follow
// the order and requirements of the unlabeled parameters.
// TODO: enable dc and peer support when ready, currently return errors if set.
func processPodLabeledDestination(pod corev1.Pod, rawUpstream string, enablePartitions, enableNamespaces bool) (*pbmesh.Destination, error) {
	parts := strings.SplitN(rawUpstream, ":", 3)
	var port int32
	port, _ = PortValue(pod, strings.TrimSpace(parts[1]))
	if port <= 0 {
		return nil, fmt.Errorf("port value %d in destination is invalid: %s", port, rawUpstream)
	}

	service := parts[0]
	pieces := strings.Split(service, ".")

	var portName, datacenter, svcName, namespace, partition string
	if enablePartitions || enableNamespaces {
		switch len(pieces) {
		case 8:
			end := strings.TrimSpace(pieces[7])
			switch end {
			case "peer":
				// TODO: uncomment and remove error when peers supported
				//peer = strings.TrimSpace(pieces[6])
				return nil, fmt.Errorf("destination currently does not support peers: %s", rawUpstream)
			case "ap":
				partition = strings.TrimSpace(pieces[6])
			case "dc":
				// TODO: uncomment and remove error when datacenters are supported
				//datacenter = strings.TrimSpace(pieces[6])
				return nil, fmt.Errorf("destination currently does not support datacenters: %s", rawUpstream)
			default:
				return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
			}
			fallthrough
		case 6:
			if strings.TrimSpace(pieces[5]) == "ns" {
				namespace = strings.TrimSpace(pieces[4])
			} else {
				return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
			}
			fallthrough
		case 4:
			if strings.TrimSpace(pieces[3]) == "svc" {
				svcName = strings.TrimSpace(pieces[2])
			} else {
				return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
			}
			if strings.TrimSpace(pieces[1]) == "port" {
				portName = strings.TrimSpace(pieces[0])
			} else {
				return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
			}
		default:
			return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
		}
	} else {
		switch len(pieces) {
		case 6:
			end := strings.TrimSpace(pieces[5])
			switch end {
			case "peer":
				// TODO: uncomment and remove error when peers supported
				//peer = strings.TrimSpace(pieces[4])
				return nil, fmt.Errorf("destination currently does not support peers: %s", rawUpstream)
			case "dc":
				// TODO: uncomment and remove error when datacenter supported
				//datacenter = strings.TrimSpace(pieces[4])
				return nil, fmt.Errorf("destination currently does not support datacenters: %s", rawUpstream)
			default:
				return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
			}
			// TODO: uncomment and remove error when datacenter and/or peers supported
			//fallthrough
		case 4:
			if strings.TrimSpace(pieces[3]) == "svc" {
				svcName = strings.TrimSpace(pieces[2])
			} else {
				return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
			}
			if strings.TrimSpace(pieces[1]) == "port" {
				portName = strings.TrimSpace(pieces[0])
			} else {
				return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
			}
		default:
			return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
		}
	}

	destination := pbmesh.Destination{
		DestinationRef: &pbresource.Reference{
			Type: pbcatalog.ServiceType,
			Tenancy: &pbresource.Tenancy{
				Partition: constants.GetNormalizedConsulPartition(partition),
				Namespace: constants.GetNormalizedConsulNamespace(namespace),
			},
			Name: svcName,
		},
		DestinationPort: portName,
		Datacenter:      datacenter,
		ListenAddr: &pbmesh.Destination_IpPort{
			IpPort: &pbmesh.IPPortAddress{
				Port: uint32(port),
				Ip:   ConsulNodeAddress,
			},
		},
	}

	return &destination, nil
}

// processPodUnlabeledDestination processes a destination in the format:
// [service-port-name].[service-name].[service-namespace].[service-partition]:[port]:[optional datacenter].
// There is no unlabeled field for peering.
// TODO: enable dc and peer support when ready, currently return errors if set.
func processPodUnlabeledDestination(pod corev1.Pod, rawUpstream string, enablePartitions, enableNamespaces bool) (*pbmesh.Destination, error) {
	var portName, datacenter, svcName, namespace, partition string
	var port int32
	var destination pbmesh.Destination

	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = PortValue(pod, strings.TrimSpace(parts[1]))

	// If Consul Namespaces or Admin Partitions are enabled, attempt to parse the
	// destination for a namespace.
	if enableNamespaces || enablePartitions {
		pieces := strings.SplitN(parts[0], ".", 4)
		switch len(pieces) {
		case 4:
			partition = strings.TrimSpace(pieces[3])
			fallthrough
		case 3:
			namespace = strings.TrimSpace(pieces[2])
			fallthrough
		case 2:
			svcName = strings.TrimSpace(pieces[1])
			portName = strings.TrimSpace(pieces[0])
		default:
			return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
		}
	} else {
		pieces := strings.SplitN(parts[0], ".", 2)
		if len(pieces) < 2 {
			return nil, fmt.Errorf("destination structured incorrectly: %s", rawUpstream)
		}
		svcName = strings.TrimSpace(pieces[1])
		portName = strings.TrimSpace(pieces[0])
	}

	// parse the optional datacenter
	if len(parts) > 2 {
		// TODO: uncomment and remove error when datacenters supported
		//datacenter = strings.TrimSpace(parts[2])
		return nil, fmt.Errorf("destination currently does not support datacenters: %s", rawUpstream)
	}

	if port > 0 {
		destination = pbmesh.Destination{
			DestinationRef: &pbresource.Reference{
				Type: pbcatalog.ServiceType,
				Tenancy: &pbresource.Tenancy{
					Partition: constants.GetNormalizedConsulPartition(partition),
					Namespace: constants.GetNormalizedConsulNamespace(namespace),
				},
				Name: svcName,
			},
			DestinationPort: portName,
			Datacenter:      datacenter,
			ListenAddr: &pbmesh.Destination_IpPort{
				IpPort: &pbmesh.IPPortAddress{
					Port: uint32(port),
					Ip:   ConsulNodeAddress,
				},
			},
		}
	}
	return &destination, nil
}
