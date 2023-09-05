// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package pod

import (
	"context"
	"fmt"
	"strconv"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	metaKeyManagedBy    = "managed-by"
	tokenMetaPodNameKey = "pod"
)

type Controller struct {
	client.Client
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	// Only pods in the AllowK8sNamespacesSet are reconciled.
	AllowK8sNamespacesSet mapset.Set
	// Pods in the DenyK8sNamespacesSet are ignored.
	DenyK8sNamespacesSet mapset.Set
	// EnableConsulPartitions indicates that a user is running Consul Enterprise
	EnableConsulPartitions bool
	// ConsulPartition is the Consul Partition to which this controller belongs
	ConsulPartition string
	// EnableConsulNamespaces indicates that a user is running Consul Enterprise
	EnableConsulNamespaces bool
	// ConsulDestinationNamespace is the name of the Consul namespace to create
	// all config entries in. If EnableNSMirroring is true this is ignored.
	ConsulDestinationNamespace string
	// EnableNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any config entry custom resource. Config entries will
	// be created in the matching Consul namespace.
	EnableNSMirroring bool
	// NSMirroringPrefix is an optional prefix that can be added to the Consul
	// namespaces created while mirroring. For example, if it is set to "k8s-",
	// then the k8s `default` namespace will be mirrored in Consul's
	// `k8s-default` namespace.
	NSMirroringPrefix string

	// TODO: EnableWANFederation

	// AuthMethod is the name of the Kubernetes Auth Method that
	// was used to login with Consul. The Endpoints controller
	// will delete any tokens associated with this auth method
	// whenever service instances are deregistered.
	AuthMethod string

	// EnableTelemetryCollector controls whether the proxy service should be registered
	// with config to enable telemetry forwarding.
	EnableTelemetryCollector bool

	MetricsConfig metrics.Config

	Log logr.Logger

	// ResourceClient is a gRPC client for the resource service. It is public for testing purposes
	ResourceClient pbresource.ResourceServiceClient
}

// TODO(dans): logs, logs, logs

// Reconcile reads the state of an Endpoints object for a Kubernetes Service and reconciles Consul services which
// correspond to the Kubernetes Service. These events are driven by changes to the Pods backing the Kube service.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var errs error
	var pod corev1.Pod

	// Ignore the request if the namespace of the endpoint is not allowed.
	// Strictly speaking, this is not required because the mesh webhook also knows valid namespaces
	// for injection, but it will somewhat reduce the amount of unnecessary deletions for non-injected
	// pods
	if common.ShouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	rc, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create resource client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	r.ResourceClient = rc

	err = r.Client.Get(ctx, req.NamespacedName, &pod)

	// If the pod object has been deleted (and we get an IsNotFound error),
	// we need to remove the Workload from Consul.
	if k8serrors.IsNotFound(err) {

		if err := r.deleteWorkload(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		// TODO: delete explicit upstreams
		//if err := r.deleteUpstreams(ctx, pod); err != nil {
		//	errs = multierror.Append(errs, err)
		//}

		// TODO(dans): delete proxyConfiguration
		//if err := r.deleteProxyConfiguration(ctx, pod); err != nil {
		//	errs = multierror.Append(errs, err)
		//}

		// TODO: clean up ACL Tokens

		// TODO(dans): delete health status, since we don't have finalizers
		//if err := r.deleteHealthStatus(ctx, pod); err != nil {
		//	errs = multierror.Append(errs, err)
		//}

		return ctrl.Result{}, errs
	} else if err != nil {
		r.Log.Error(err, "failed to get Pod", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved", "name", pod.Name, "ns", pod.Namespace)

	if hasBeenInjected(pod) {
		if err := r.writeWorkload(ctx, pod); err != nil {
			errs = multierror.Append(errs, err)
		}

		// TODO(dans): create proxyConfiguration

		// TODO: create explicit upstreams
		//if err := r.writeUpstreams(ctx, pod); err != nil {
		//	errs = multierror.Append(errs, err)
		//}

		// TODO(dans): write health status
		//if err := r.writeHealthStatus(ctx, pod); err != nil {
		//	errs = multierror.Append(errs, err)
		//}
	}

	return ctrl.Result{}, errs
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

// hasBeenInjected checks the value of the status annotation and returns true if the Pod has been injected.
func hasBeenInjected(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[constants.KeyMeshInjectStatus]; ok && anno == constants.Injected {
		return true
	}
	return false
}

func (r *Controller) deleteWorkload(ctx context.Context, pod types.NamespacedName) error {
	req := &pbresource.DeleteRequest{
		Id: getWorkloadID(pod.Name, r.getConsulNamespace(pod.Namespace), r.getPartition()),
	}

	_, err := r.ResourceClient.Delete(ctx, req)
	return err
}

//func (r *Controller) deleteHealthStatus(ctx context.Context, pod corev1.Pod) error {
//	return nil
//}

func (r *Controller) writeWorkload(ctx context.Context, pod corev1.Pod) error {

	// TODO: we should add some validation on the required fields here
	// e.g. what if token automount is disabled and there is not SA. The API call
	// will fail with no indication to the user other than controller logs
	ports, workloadPorts := getWorkloadPorts(pod)

	var node corev1.Node
	// Ignore errors because we don't want failures to block running services.
	_ = r.Client.Get(context.Background(), types.NamespacedName{Name: pod.Spec.NodeName, Namespace: pod.Namespace}, &node)
	locality := parseLocality(node)

	workload := &pbcatalog.Workload{
		Addresses: []*pbcatalog.WorkloadAddress{
			{Host: pod.Status.PodIP, Ports: ports},
		},
		Identity: pod.Spec.ServiceAccountName,
		Locality: locality,
		NodeName: common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName),
		Ports:    workloadPorts,
	}

	// TODO(dans): replace with common.ToProtoAny when available
	proto, err := anypb.New(workload)
	if err != nil {
		return fmt.Errorf("could not serialize workload: %w", err)
	}

	// TODO: allow custom workload metadata
	meta := map[string]string{
		constants.MetaKeyKubeNS: pod.Namespace,
		metaKeyManagedBy:        constants.ManagedByPodValue,
	}

	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:       getWorkloadID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()),
			Metadata: meta,
			Data:     proto,
		},
	}
	_, err = r.ResourceClient.Write(ctx, req)
	return err
}

//func (r *Controller) writeHealthStatus(pod corev1.Pod) error {
//	return nil
//}

// TODO(dans): delete ACL token for workload
// deleteACLTokensForServiceInstance finds the ACL tokens that belongs to the service instance and deletes it from Consul.
// It will only check for ACL tokens that have been created with the auth method this controller
// has been configured with and will only delete tokens for the provided podName.
// func (r *Controller) deleteACLTokensForWorkload(apiClient *api.Client, svc *api.AgentService, k8sNS, podName string) error {

// TODO: add support for explicit upstreams
//func (r *Controller) writeUpstreams(pod corev1.Pod) error

// consulNamespace returns the Consul destination namespace for a provided Kubernetes namespace
// depending on Consul Namespaces being enabled and the value of namespace mirroring.
func (r *Controller) getConsulNamespace(kubeNamespace string) string {
	ns := namespaces.ConsulNamespace(
		kubeNamespace,
		r.EnableConsulNamespaces,
		r.ConsulDestinationNamespace,
		r.EnableNSMirroring,
		r.NSMirroringPrefix,
	)

	// TODO: remove this if and when the default namespace of resources change.
	if ns == "" {
		ns = constants.DefaultConsulNS
	}
	return ns
}

func (r *Controller) getPartition() string {
	if !r.EnableConsulPartitions || r.ConsulPartition == "" {
		return constants.DefaultConsulPartition
	}
	return r.ConsulPartition
}

func getWorkloadPorts(pod corev1.Pod) ([]string, map[string]*pbcatalog.WorkloadPort) {
	ports := make([]string, 0)
	workloadPorts := map[string]*pbcatalog.WorkloadPort{}

	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			name := port.Name
			if name == "" {
				name = strconv.Itoa(int(port.ContainerPort))
			}

			// TODO: error check reserved "mesh" keyword and 20000

			if port.Protocol != corev1.ProtocolTCP {
				// TODO: also throw an error here
				continue
			}

			ports = append(ports, name)
			workloadPorts[name] = &pbcatalog.WorkloadPort{
				Port: uint32(port.ContainerPort),

				// We leave the protocol unspecified so that it can be inherited from the Service appProtocol
				Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
			}
		}
	}

	ports = append(ports, "mesh")
	workloadPorts["mesh"] = &pbcatalog.WorkloadPort{
		Port:     constants.ProxyDefaultInboundPort,
		Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
	}

	return ports, workloadPorts
}

func parseLocality(node corev1.Node) *pbcatalog.Locality {
	region := node.Labels[corev1.LabelTopologyRegion]
	zone := node.Labels[corev1.LabelTopologyZone]

	if region == "" {
		return nil
	}

	return &pbcatalog.Locality{
		Region: region,
		Zone:   zone,
	}
}

func getWorkloadID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: &pbresource.Type{
			Group:        "catalog",
			GroupVersion: "v1alpha1",
			Kind:         "Workload",
		},
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}
