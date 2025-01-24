// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpointsv2

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	kindReplicaSet = "ReplicaSet"
)

type Controller struct {
	client.Client
	// ConsulServerConnMgr is the watcher for the Consul server addresses used to create Consul API v2 clients.
	ConsulServerConnMgr consul.ServerConnectionManager
	// K8sNamespaceConfig manages allow/deny Kubernetes namespaces.
	common.K8sNamespaceConfig
	// ConsulTenancyConfig manages settings related to Consul namespaces and partitions.
	common.ConsulTenancyConfig

	// WriteCache keeps track of records already written to Consul in order to enable debouncing of writes.
	// This is useful in particular for this controller which will see potentially many more reconciles due to
	// endpoint changes (e.g. pod health) than changes to service data written to Consul.
	// It is intentionally simple and best-effort, and does not guarantee against all redundant writes.
	// It is not persistent across restarts of the controller process.
	WriteCache WriteCache

	Log logr.Logger

	Scheme *runtime.Scheme
	context.Context
}

func (r *Controller) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	if r.WriteCache == nil {
		return fmt.Errorf("WriteCache was not configured for Controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(r)
}

// Reconcile reads the state of an Endpoints object for a Kubernetes Service and reconciles Consul services which
// correspond to the Kubernetes Service. These events are driven by changes to the Pods backing the Kube service.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var endpoints corev1.Endpoints
	var service corev1.Service

	// Ignore the request if the namespace of the endpoint is not allowed.
	if inject.ShouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	// Create Consul resource service client for this reconcile.
	resourceClient, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create Consul resource client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	state, err := r.ConsulServerConnMgr.State()
	if err != nil {
		r.Log.Error(err, "failed to query Consul client state", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	if state.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-consul-token", state.Token)
	}

	// If the Endpoints object has been deleted (and we get an IsNotFound error),
	// we need to deregister that service from Consul.
	err = r.Client.Get(ctx, req.NamespacedName, &endpoints)
	if k8serrors.IsNotFound(err) {
		err = r.deregisterService(ctx, resourceClient, req)
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

	consulSvc, err := r.getConsulService(ctx, &ClientPodFetcher{client: r.Client}, service, endpoints)
	if err != nil {
		r.Log.Error(err, "failed to build Consul service resource", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	// If we don't have at least one mesh-injected pod selected by the service, don't register.
	// Note that we only _delete_ services when they're deleted from K8s, not when endpoints or
	// workload selectors are empty. This ensures that failover can occur normally when targeting
	// the existing VIP (ClusterIP) assigned to the service.
	if consulSvc.Workloads == nil {
		return ctrl.Result{}, nil
	}

	// Register the service in Consul.
	id := getServiceID(
		service.Name, // Consul and Kubernetes service name will always match
		r.getConsulNamespace(service.Namespace),
		r.getConsulPartition())
	meta := getServiceMeta(service)
	k8sUid := string(service.UID)
	if err = r.ensureService(ctx, &defaultResourceReadWriter{resourceClient}, k8sUid, id, meta, consulSvc); err != nil {
		// We could be racing with the namespace controller.
		// Requeue (which includes backoff) to try again.
		if inject.ConsulNamespaceIsNotFound(err) {
			r.Log.Info("Consul namespace not found; re-queueing request",
				"service", service.GetName(), "ns", req.Namespace,
				"consul-ns", r.getConsulNamespace(req.Namespace), "err", err.Error())
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Controller) getConsulService(ctx context.Context, pf PodFetcher, service corev1.Service, endpoints corev1.Endpoints) (*pbcatalog.Service, error) {
	prefixedPods, exactNamePods, err := r.getWorkloadDataFromEndpoints(ctx, pf, endpoints)
	if err != nil {
		return nil, err
	}

	// Create Consul Service resource to be registered.
	return &pbcatalog.Service{
		Workloads:  getWorkloadSelector(prefixedPods, exactNamePods),
		Ports:      getServicePorts(service, prefixedPods, exactNamePods),
		VirtualIps: r.getServiceVIPs(service),
	}, nil
}

type podSetData struct {
	podCount  int
	samplePod *corev1.Pod
}

// selectorPodData represents data for each set of pods represented by a WorkloadSelector value.
// The data may be for several pods (prefix) or a single pod (exact name).
// This is used for choosing the ideal Consul service TargetPort value when the K8s service target port is numeric.
type selectorPodData map[string]*podSetData

// getWorkloadDataFromEndpoints accumulates data to supply the Consul service WorkloadSelector and TargetPort from
// Endpoints based on pod names and owners.
func (r *Controller) getWorkloadDataFromEndpoints(ctx context.Context, pf PodFetcher, endpoints corev1.Endpoints) (selectorPodData, selectorPodData, error) {
	var errs error

	// Determine the workload selector by fetching as many pods as needed to accumulate prefixes
	// and exact pod name matches.
	//
	// If the K8s service target port is numeric, we also use this information to determine the
	// appropriate Consul target port value.
	prefixedPods := make(selectorPodData)
	exactNamePods := make(selectorPodData)
	ignoredPodPrefixes := make(map[string]any)
	for address := range allAddresses(endpoints.Subsets) {
		if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
			podName := types.NamespacedName{Name: address.TargetRef.Name, Namespace: endpoints.Namespace}

			// Accumulate owner prefixes and exact pod names for Consul workload selector.
			// If this pod is already covered by a known owner prefix, skip it.
			// If not, fetch the owner. If the owner has a unique prefix, add it to known prefixes.
			// If not, add the pod name to exact name matches.
			maybePodOwnerPrefix := getOwnerPrefixFromPodName(podName.Name)

			// If prefix is ignored, skip pod.
			if _, ok := ignoredPodPrefixes[maybePodOwnerPrefix]; ok {
				continue
			}

			if existingPodData, ok := prefixedPods[maybePodOwnerPrefix]; !ok {
				// Fetch pod info from K8s.
				pod, err := pf.GetPod(ctx, podName)
				if err != nil {
					r.Log.Error(err, "failed to get pod", "name", podName.Name, "ns", endpoints.Namespace)
					errs = multierror.Append(errs, err)
					continue
				}

				// Store data corresponding to the new selector value, which may be an actual set or exact pod.
				podData := podSetData{
					podCount:  1,
					samplePod: pod,
				}

				// Add pod to workload selector values as appropriate.
				// Pods can appear more than once in Endpoints subsets, so we use a set for exact names as well.
				if prefix := getOwnerPrefixFromPod(pod); prefix != "" {
					if inject.HasBeenMeshInjected(*pod) {
						// Add to the list of pods represented by this prefix. This list is used by
						// `getEffectiveTargetPort` to determine the most-used target container port name if the
						// k8s service target port is numeric.
						prefixedPods[prefix] = &podData
					} else {
						// If the pod hasn't been mesh-injected, ignore it, as it won't be available as a workload.
						// Remember its prefix to avoid fetching its siblings needlessly.
						ignoredPodPrefixes[prefix] = true
					}
				} else {
					if inject.HasBeenMeshInjected(*pod) {
						exactNamePods[podName.Name] = &podData
					}
					// If the pod hasn't been mesh-injected, ignore it, as it won't be available as a workload.
					// No need to remember ignored exact pod names since we don't expect to see them twice.
				}
			} else {
				// We've seen this prefix before.
				// Keep track of how many times so that we can choose a container port name if needed later.
				existingPodData.podCount += 1
			}
		}
	}

	return prefixedPods, exactNamePods, errs
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

// ensureService upserts a Consul service resource if an identical write has not already been made to Consul since this
// controller was started. If the check for a previous write fails, the resource is written anyway.
func (r *Controller) ensureService(ctx context.Context, rw resourceReadWriter, k8sUid string, id *pbresource.ID, meta map[string]string, consulSvc *pbcatalog.Service) error {
	// Use Marshal w/ Deterministic option to ensure write hash generated from Data is consistent.
	data := new(anypb.Any)
	if err := anypb.MarshalFrom(data, consulSvc, proto.MarshalOptions{Deterministic: true}); err != nil {
		return err
	}

	// Use the locally-created Resource and ID (without Uid and Version) when writing so that it
	// behaves as an upsert rather than CAS.
	consulSvcResource := &pbresource.Resource{
		Id:       id,
		Data:     data,
		Metadata: meta,
	}

	writeHash, err := getWriteHash(consulSvcResource)
	if err != nil {
		r.Log.Error(err, "failed to get write hash for service; assuming it is out of sync",
			getLogFieldsForResource(id)...)
	}
	key := getWriteCacheKey(types.NamespacedName{Name: id.Name, Namespace: id.Tenancy.Namespace})
	generationFetchFn := func() string {
		// Check for whether a matching service already exists in Consul.
		// Gracefully fail on error. This allows us to make a best-effort write attempt in
		// case of a persistent read error or permissions issue that does not impact writing.
		resp, err := rw.Read(ctx, &pbresource.ReadRequest{Id: id})
		if s, ok := status.FromError(err); !ok || (s.Code() != codes.OK && s.Code() != codes.NotFound) {
			r.Log.Error(err, "failed to read existing service resource from Consul; assuming it is out of sync",
				append(getLogFieldsForResource(id), "code", s.Code(), "message", s.Message())...)
			return ""
		}
		return resp.GetResource().GetGeneration()
	}
	if r.WriteCache.hasMatch(key, writeHash, generationFetchFn, k8sUid) {
		r.Log.V(1).Info("skipping service write due to matching write hash")
		return nil
	}

	r.Log.Info("writing service to Consul", getLogFieldsForResource(consulSvcResource.Id)...)
	resp, err := rw.Write(ctx, &pbresource.WriteRequest{Resource: consulSvcResource})
	if err != nil {
		r.Log.Error(err, fmt.Sprintf("failed to write service: %+v", consulSvc),
			getLogFieldsForResource(consulSvcResource.Id)...)
		return err
	}

	generation := resp.GetResource().GetGeneration()
	r.Log.Info("caching service write to Consul", "hash", writeHash, "generation", generation,
		"k8sUid", k8sUid)
	r.WriteCache.update(key, writeHash, generation, k8sUid)

	return nil
}

// resourceReadWriter wraps pbresource.ResourceServiceClient for testing purposes.
// The default implementation is a passthrough used outside of tests.
type resourceReadWriter interface {
	Read(context.Context, *pbresource.ReadRequest) (*pbresource.ReadResponse, error)
	Write(context.Context, *pbresource.WriteRequest) (*pbresource.WriteResponse, error)
}

type defaultResourceReadWriter struct {
	client pbresource.ResourceServiceClient
}

func (c *defaultResourceReadWriter) Read(ctx context.Context, req *pbresource.ReadRequest) (*pbresource.ReadResponse, error) {
	return c.client.Read(ctx, req)
}

func (c *defaultResourceReadWriter) Write(ctx context.Context, req *pbresource.WriteRequest) (*pbresource.WriteResponse, error) {
	return c.client.Write(ctx, req)
}

func getServiceID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbcatalog.ServiceType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

// getServicePorts converts Kubernetes Service ports data into Consul service ports.
func getServicePorts(service corev1.Service, prefixedPods selectorPodData, exactNamePods selectorPodData) []*pbcatalog.ServicePort {
	ports := make([]*pbcatalog.ServicePort, 0, len(service.Spec.Ports)+1)

	for _, p := range service.Spec.Ports {
		// Service mesh only supports TCP as the L4 Protocol (not to be confused w/ L7 AppProtocol).
		//
		// This check is necessary to deduplicate VirtualPort values when multiple declared ServicePort values exist
		// for the same port, which is possible in K8s when e.g. multiplexing TCP and UDP traffic over a single port.
		//
		// If we otherwise see repeat port values in a K8s service, we pass along and allow Consul to fail validation.
		if p.Protocol == corev1.ProtocolTCP {
			//TODO(NET-5705): Error check reserved "mesh" target port
			ports = append(ports, &pbcatalog.ServicePort{
				VirtualPort: uint32(p.Port),
				TargetPort:  getEffectiveTargetPort(p.TargetPort, prefixedPods, exactNamePods),
				Protocol:    inject.GetPortProtocol(p.AppProtocol),
			})
		}
	}

	// Sort for comparison stability during write deduplication.
	sort.Slice(ports, func(i, j int) bool {
		return ports[i].VirtualPort < ports[j].VirtualPort
	})

	// Append Consul service mesh port in addition to discovered ports.
	ports = append(ports, &pbcatalog.ServicePort{
		TargetPort: "mesh",
		Protocol:   pbcatalog.Protocol_PROTOCOL_MESH,
	})

	return ports
}

func getEffectiveTargetPort(targetPort intstr.IntOrString, prefixedPods selectorPodData, exactNamePods selectorPodData) string {
	// The Kubernetes service is targeting a port name; use it directly.
	// The expected behavior of Kubernetes is that all included Endpoints conform and have a matching named port.
	// This is the simplest path and preferred over services targeting by port number.
	if targetPort.Type == intstr.String {
		return targetPort.String()
	}

	// The Kubernetes service is targeting a numeric port. This is more complicated for mapping to Consul:
	//  - Endpoints will contain _all_ selected pods, not just those with a matching declared port number.
	//  - Consul Workload ports always have a name, so we must determine the best name to match on.
	//  - There may be more than one option among the pods with named ports, including no name at all.
	//
	// Our best-effort approach is to find the most prevalent port name among selected pods that _do_ declare the target
	// port explicitly in container ports. We'll assume that for each set of pods, the first pod is "representative" -
	// i.e. we expect a ReplicaSet to be homogenous. In the vast majority of cases, this means we'll be looking for the
	// largest selected ReplicaSet and using the first pod from it.
	//
	// The goal is to make this determination without fetching all pods belonging to the service, as that would be a
	// very expensive operation to repeat every time endpoints change, and we don't expect the target port to change
	// often if ever across pod/deployment lifecycles.
	//
	//TODO(NET-5706) in GA, we intend to change port selection to allow for Consul TargetPort to be numeric. If we
	// retain the port selection model used here beyond GA, we should consider updating it to also consider pod health,
	// s.t. when the selected port name changes between deployments of a ReplicaSet, we route traffic to ports
	// belonging to the set most able to serve traffic, rather than simply the largest one.
	targetPortInt := int32(targetPort.IntValue())
	var mostPrevalentContainerPort *corev1.ContainerPort
	maxCount := 0
	effectiveNameForPort := inject.WorkloadPortName
	for _, podData := range prefixedPods {
		containerPort := getTargetContainerPort(targetPortInt, podData.samplePod)

		// Ignore pods without a declared port matching the service targetPort.
		if containerPort == nil {
			continue
		}

		// If this is the most prevalent container port by pod set size, update result.
		if maxCount < podData.podCount {
			mostPrevalentContainerPort = containerPort
			maxCount = podData.podCount
		}
	}

	if mostPrevalentContainerPort != nil {
		return effectiveNameForPort(mostPrevalentContainerPort)
	}

	// If no pod sets have the expected target port, fall back to the most common name among exact-name pods.
	// An assumption here is that exact name pods mixed with pod sets will be rare, and sets should be preferred.
	if len(exactNamePods) > 0 {
		nameCount := make(map[string]int)
		for _, podData := range exactNamePods {
			if containerPort := getTargetContainerPort(targetPortInt, podData.samplePod); containerPort != nil {
				nameCount[effectiveNameForPort(containerPort)] += 1
			}
		}
		if len(nameCount) > 0 {
			maxNameCount := 0
			mostPrevalentContainerPortName := ""
			for name, count := range nameCount {
				if maxNameCount < count {
					mostPrevalentContainerPortName = name
					maxNameCount = count
				}
			}
			return mostPrevalentContainerPortName
		}
	}

	// If still no match for the target port, fall back to string-ifying the target port name, which
	// is what the PodController will do when converting unnamed ContainerPorts to Workload ports.
	return constants.UnnamedWorkloadPortNamePrefix + targetPort.String()
}

// getTargetContainerPort returns the pod ContainerPort matching the given numeric port value, or nil if none is found.
func getTargetContainerPort(targetPort int32, pod *corev1.Pod) *corev1.ContainerPort {
	for _, c := range pod.Spec.Containers {
		if len(c.Ports) == 0 {
			continue
		}
		for _, p := range c.Ports {
			if p.ContainerPort == targetPort && p.Protocol == corev1.ProtocolTCP {
				return &p
			}
		}
	}
	return nil
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

	// Note: This slice needs to be sorted for stable comparison during write deduplication.
	// If additional values are added in the future, the output order should be consistent.
	return []string{service.Spec.ClusterIP}
}

func getServiceMeta(service corev1.Service) map[string]string {
	meta := map[string]string{
		constants.MetaKeyKubeNS:    service.Namespace,
		constants.MetaKeyManagedBy: constants.ManagedByEndpointsValue,
	}
	return meta
}

// getWorkloadSelector returns the WorkloadSelector for the given pod name prefixes and exact names.
// It returns nil if the provided name sets are empty.
func getWorkloadSelector(prefixedPods selectorPodData, exactNamePods selectorPodData) *pbcatalog.WorkloadSelector {
	// If we don't have any values, return nil
	if len(prefixedPods) == 0 && len(exactNamePods) == 0 {
		return nil
	}

	// Create the WorkloadSelector
	workloads := &pbcatalog.WorkloadSelector{}
	for v := range prefixedPods {
		workloads.Prefixes = append(workloads.Prefixes, v)
	}
	for v := range exactNamePods {
		workloads.Names = append(workloads.Names, v)
	}

	// Sort for comparison stability during write deduplication
	sort.Strings(workloads.Prefixes)
	sort.Strings(workloads.Names)

	return workloads
}

// deregisterService deletes the service resource corresponding to the given name and namespace from Consul.
// This operation is idempotent and can be executed for non-existent services.
func (r *Controller) deregisterService(ctx context.Context, resourceClient pbresource.ResourceServiceClient, req ctrl.Request) error {
	// Regardless of whether we get an error on delete, remove the resource from the cache as we intend for it
	// to be deleted and the record is no longer valid for preventing writes.
	r.WriteCache.remove(getWriteCacheKey(req.NamespacedName))
	_, err := resourceClient.Delete(ctx, &pbresource.DeleteRequest{
		Id: getServiceID(req.Name, r.getConsulNamespace(req.Namespace), r.getConsulPartition()),
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

	// TODO(NET-5652): remove this if and when the default namespace of resources is no longer required to be set explicitly.
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

// getWriteCacheKey gets a key to track syncronization of a K8s service to deduplicate writes to Consul.
// See also WriteCache.hasMatch.
func getWriteCacheKey(serviceName types.NamespacedName) string {
	return serviceName.String()
}

// getWriteHash gets a hash of the given resource to deduplicate writes to Consul.
//
// This hash is not intended to be cryptographically secure, only deterministic and collision-resistent
// for tens of thousands of values.
//
// If an error occurs marshalling the resource for the hash, returns a nil hash value and the error.
// error will be returned.
func getWriteHash(r *pbresource.Resource) ([]byte, error) {
	// We Marshal the entire resource (not just its own Data, which is already serialized)
	// in order to take advantage of the deterministic marshal offered by proto and include
	// fields like Meta, which are not part of the resource Data.
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(data)
	return h[:], nil
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
