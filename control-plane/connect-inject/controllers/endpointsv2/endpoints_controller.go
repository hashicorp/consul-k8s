// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package endpointsv2

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/go-multierror"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	metaKeyManagedBy = "managed-by"
	kindReplicaSet   = "ReplicaSet"
)

type Controller struct {
	client.Client
	// ConsulServerConnMgr is the watcher for the Consul server addresses used to create Consul API v2 clients.
	ConsulServerConnMgr consul.ServerConnectionManager
	// Only endpoints in the AllowK8sNamespacesSet are reconciled.
	AllowK8sNamespacesSet mapset.Set
	// Endpoints in the DenyK8sNamespacesSet are ignored.
	DenyK8sNamespacesSet mapset.Set
	// EnableConsulPartitions indicates that a user is running Consul Enterprise.
	EnableConsulPartitions bool
	// ConsulPartition is the Consul Partition to which this controller belongs.
	ConsulPartition string
	// EnableConsulNamespaces indicates that a user is running Consul Enterprise.
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

	Log logr.Logger

	Scheme *runtime.Scheme
	context.Context
}

func (r *Controller) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(r)
}

// Reconcile reads the state of an Endpoints object for a Kubernetes Service and reconciles Consul services which
// correspond to the Kubernetes Service. These events are driven by changes to the Pods backing the Kube service.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var errs error
	var endpoints corev1.Endpoints
	var service corev1.Service

	// Ignore the request if the namespace of the endpoint is not allowed.
	if common.ShouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	// Create Consul resource service client for this reconcile.
	resourceClient, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create Consul resource client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	// If the Endpoints object has been deleted (and we get an IsNotFound error),
	// we need to deregister that service from Consul.
	err = r.Client.Get(ctx, req.NamespacedName, &endpoints)
	if k8serrors.IsNotFound(err) {
		err = r.deregisterService(ctx, resourceClient, req.Name, r.getConsulNamespace(req.Namespace), r.getConsulPartition())
		return ctrl.Result{}, err
	} else if err != nil {
		r.Log.Error(err, "failed to get Endpoints", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	r.Log.Info("retrieved Endpoints", "name", req.Name, "ns", req.Namespace)

	// We expect this to succeed if the Endpoints fetch for the Service succeeded.
	err = r.Client.Get(r.Context, types.NamespacedName{Name: endpoints.Name, Namespace: endpoints.Namespace}, &service)
	if err != nil {
		r.Log.Error(err, "failed to get Service", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	r.Log.Info("retrieved Service", "name", req.Name, "ns", req.Namespace)

	workloadSelector, err := r.getWorkloadSelectorFromEndpoints(ctx, &ClientPodFetcher{client: r.Client}, &endpoints)
	if err != nil {
		errs = multierror.Append(errs, err)
	}

	//TODO: Maybe check service-enable label here on service/deployments/other pod owners
	if err = r.registerService(ctx, resourceClient, service, workloadSelector); err != nil {
		errs = multierror.Append(errs, err)
	}

	return ctrl.Result{}, errs
}

// getWorkloadSelectorFromEndpoints calculates a Consul service WorkloadSelector from Endpoints based on pod names and
// owners.
func (r *Controller) getWorkloadSelectorFromEndpoints(ctx context.Context, pf PodFetcher, endpoints *corev1.Endpoints) (*pbcatalog.WorkloadSelector, error) {
	podPrefixes := make(map[string]any)
	podExactNames := make(map[string]any)
	var errs error
	for address := range allAddresses(endpoints.Subsets) {
		if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
			podName := types.NamespacedName{Name: address.TargetRef.Name, Namespace: endpoints.Namespace}

			// Accumulate owner prefixes and exact pod names for Consul workload selector.
			// If this pod is already covered by a known owner prefix, skip it.
			// If not, fetch the owner. If the owner has a unique prefix, add it to known prefixes.
			// If not, add the pod name to exact name matches.
			maybePodOwnerPrefix := getOwnerPrefixFromPodName(podName.Name)
			if _, ok := podPrefixes[maybePodOwnerPrefix]; !ok {
				pod, err := pf.GetPod(ctx, podName)
				if err != nil {
					r.Log.Error(err, "failed to get pod", "name", podName.Name, "ns", endpoints.Namespace)
					errs = multierror.Append(errs, err)
					continue
				}
				// Add to workload selector values.
				// Pods can appear more than once in Endpoints subsets, so we use a set for exact names as well.
				if prefix := getOwnerPrefixFromPod(pod); prefix != "" {
					podPrefixes[prefix] = true
				} else {
					podExactNames[podName.Name] = true
				}
			}
		}
	}
	return getWorkloadSelector(podPrefixes, podExactNames), errs
}

// allAddresses combines all Endpoints subset addresses to a single set. Service registration by this controller
// operates independent of health, and an address can appear in multiple subsets if it has a mix of ready and not-ready
// ports, so we combine them here to simplify iteration.
func allAddresses(subsets []corev1.EndpointSubset) map[corev1.EndpointAddress]any {
	m := make(map[corev1.EndpointAddress]any)
	for _, sub := range subsets {
		for _, readyAddress := range sub.Addresses {
			m[readyAddress] = true
		}
		for _, notReadyAddress := range sub.NotReadyAddresses {
			m[notReadyAddress] = true
		}
	}
	return m
}

// getOwnerPrefixFromPodName extracts the owner name prefix from a pod name.
func getOwnerPrefixFromPodName(podName string) string {
	podNameParts := strings.Split(podName, "-")
	return strings.Join(podNameParts[:len(podNameParts)-1], "-")
}

// getOwnerPrefixFromPod returns the common name prefix of the pod, if the pod is a member of a set with a unique name
// prefix. Currently, this only applies to ReplicaSets.
//
// We have to fetch the owner and check its type because pod names cannot be disambiguated from pod owner names due to
// the `-` delimiter and unique ID parts also being valid name components.
//
// If the pod owner does not have a unique name, the empty string is returned.
func getOwnerPrefixFromPod(pod *corev1.Pod) string {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "ReplicaSet" {
			return ref.Name
		}
	}
	return ""
}

// registerService creates a Consul service registration from the provided Kuberetes service and endpoint information.
func (r *Controller) registerService(ctx context.Context, resourceClient pbresource.ResourceServiceClient, service corev1.Service, selector *pbcatalog.WorkloadSelector) error {
	consulSvc := &pbcatalog.Service{
		Workloads:  selector,
		Ports:      getServicePorts(service),
		VirtualIps: r.getServiceVIPs(service),
	}
	consulSvcResource := r.getServiceResource(
		consulSvc,
		service.Name, // Consul and Kubernetes service name will always match
		r.getConsulNamespace(service.Namespace),
		r.getConsulPartition(),
		getServiceMeta(service),
	)

	r.Log.Info("registering service with Consul", getLogFieldsForResource(consulSvcResource.Id)...)
	//TODO: Maybe attempt to debounce redundant writes. For now, we blindly rewrite state on each reconcile.
	_, err := resourceClient.Write(ctx, &pbresource.WriteRequest{Resource: consulSvcResource})
	if err != nil {
		r.Log.Error(err, fmt.Sprintf("failed to register service: %+v", consulSvc), getLogFieldsForResource(consulSvcResource.Id)...)
		return err
	}

	return nil
}

// getServiceResource converts the given Consul service and metadata as a Consul resource API record.
func (r *Controller) getServiceResource(svc *pbcatalog.Service, name, namespace, partition string, meta map[string]string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       getServiceID(name, namespace, partition),
		Data:     common.ToProtoAny(svc),
		Metadata: meta,
	}
}

func getServiceID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: &pbresource.Type{
			Group:        "catalog",
			GroupVersion: "v1alpha1",
			Kind:         "Service",
		},
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

// getServicePorts converts Kubernetes Service ports data into Consul service ports.
func getServicePorts(service corev1.Service) []*pbcatalog.ServicePort {
	ports := make([]*pbcatalog.ServicePort, 0, len(service.Spec.Ports)+1)

	for _, p := range service.Spec.Ports {
		// Service mesh only supports TCP as the L4 Protocol (not to be confused w/ L7 AppProtocol).
		//
		// This check is necessary to deduplicate VirtualPort values when multiple declared ServicePort values exist
		// for the same port, which is possible in K8s when e.g. multiplexing TCP and UDP traffic over a single port.
		//
		// If we otherwise see repeat port values in a K8s service, we pass along and allow Consul to fail validation.
		if p.Protocol == corev1.ProtocolTCP {
			ports = append(ports, &pbcatalog.ServicePort{
				VirtualPort: uint32(p.Port),
				//TODO: If the value is a number, infer the correct name value based on
				// the most prevalent endpoint subset for the port (best-effot, inspect a pod).
				TargetPort: p.TargetPort.String(),
				Protocol:   common.GetPortProtocol(p.AppProtocol),
			})
		}
	}

	//TODO: Error check reserved "mesh" target port

	// Append Consul service mesh port in addition to discovered ports.
	//TODO: Maybe omit if zero mesh ports present in service endpoints, or if some
	// use of mesh-inject/other label should cause this to be excluded.
	ports = append(ports, &pbcatalog.ServicePort{
		TargetPort: "mesh",
		Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
	})

	return ports
}

// getServiceVIPs returns the VIPs to associate with the registered Consul service. This will contain the Kubernetes
// Service ClusterIP if it exists.
//
// Note that we always provide this data regardless of whether TProxy is enabled, deferring to individual proxy configs
// to decide whether it's used.
func (r *Controller) getServiceVIPs(service corev1.Service) []string {
	if parsedIP := net.ParseIP(service.Spec.ClusterIP); parsedIP == nil {
		r.Log.Info("skipping service registration virtual IP assignment due to invalid or unset ClusterIP", "name", service.Name, "ns", service.Namespace, "ip", service.Spec.ClusterIP)
		return nil
	}
	return []string{service.Spec.ClusterIP}
}

func getServiceMeta(service corev1.Service) map[string]string {
	meta := map[string]string{
		constants.MetaKeyKubeNS: service.Namespace,
		metaKeyManagedBy:        constants.ManagedByEndpointsValue,
	}
	//TODO: Support arbitrary meta injection via annotation? (see v1)
	return meta
}

func getWorkloadSelector(podPrefixes, podExactNames map[string]any) *pbcatalog.WorkloadSelector {
	workloads := &pbcatalog.WorkloadSelector{}
	for v := range podPrefixes {
		workloads.Prefixes = append(workloads.Prefixes, v)
	}
	for v := range podExactNames {
		workloads.Names = append(workloads.Names, v)
	}
	// sort for stability
	sort.Strings(workloads.Prefixes)
	sort.Strings(workloads.Names)

	return workloads
}

// deregisterService deletes the service resource corresponding to the given name and namespace from Consul.
// This operation is idempotent and can be executed for non-existent services.
func (r *Controller) deregisterService(ctx context.Context, resourceClient pbresource.ResourceServiceClient, name, namespace, partition string) error {
	_, err := resourceClient.Delete(ctx, &pbresource.DeleteRequest{
		Id: getServiceID(name, namespace, partition),
	})
	return err
}

// getConsulNamespace returns the Consul destination namespace for a provided Kubernetes namespace
// depending on Consul Namespaces being enabled and the value of namespace mirroring.
func (r *Controller) getConsulNamespace(kubeNamespace string) string {
	ns := namespaces.ConsulNamespace(
		kubeNamespace,
		r.EnableConsulNamespaces,
		r.ConsulDestinationNamespace,
		r.EnableNSMirroring,
		r.NSMirroringPrefix,
	)

	// TODO: remove this if and when the default namespace of resources is no longer required to be set explicitly.
	if ns == "" {
		ns = constants.DefaultConsulNS
	}
	return ns
}

func (r *Controller) getConsulPartition() string {
	if !r.EnableConsulPartitions || r.ConsulPartition == "" {
		return constants.DefaultConsulPartition
	}
	return r.ConsulPartition
}

func getLogFieldsForResource(id *pbresource.ID) []any {
	return []any{
		"name", id.Name,
		"ns", id.Tenancy.Namespace,
		"partition", id.Tenancy.Partition,
	}
}

// PodFetcher fetches pods by NamespacedName. This interface primarily exists for testing.
type PodFetcher interface {
	GetPod(context.Context, types.NamespacedName) (*corev1.Pod, error)
}

// ClientPodFetcher wraps a Kubernetes client to implement PodFetcher. This is the only implementation outside of tests.
type ClientPodFetcher struct {
	client client.Client
}

func (c *ClientPodFetcher) GetPod(ctx context.Context, name types.NamespacedName) (*corev1.Pod, error) {
	var pod corev1.Pod
	err := c.client.Get(ctx, name, &pod)
	if err != nil {
		return nil, err
	}
	return &pod, nil
}
