// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package pod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	DefaultTelemetryBindSocketDir = "/consul/mesh-inject"
	consulNodeAddress             = "127.0.0.1"
	tokenMetaPodNameKey           = "pod"
)

// Controller watches Pod events and converts them to V2 Workloads and HealthStatus.
// The translation from Pod to Workload is 1:1 and the HealthStatus object is a representation
// of the Pod's Status field. Controller is also responsible for generating V2 Upstreams resources
// when not in transparent proxy mode. ProxyConfiguration is also optionally created.
type Controller struct {
	client.Client
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	// K8sNamespaceConfig manages allow/deny Kubernetes namespaces.
	common.K8sNamespaceConfig
	// ConsulTenancyConfig manages settings related to Consul namespaces and partitions.
	common.ConsulTenancyConfig

	// TODO: EnableWANFederation

	// EnableTransparentProxy controls whether transparent proxy should be enabled
	// for all proxy service registrations.
	EnableTransparentProxy bool
	// TProxyOverwriteProbes controls whether the pods controller should expose pod's HTTP probes
	// via Envoy proxy.
	TProxyOverwriteProbes bool

	// AuthMethod is the name of the Kubernetes Auth Method that
	// was used to login with Consul. The pods controller
	// will delete any tokens associated with this auth method
	// whenever service instances are deregistered.
	AuthMethod string

	// EnableTelemetryCollector controls whether the proxy service should be registered
	// with config to enable telemetry forwarding.
	EnableTelemetryCollector bool

	MetricsConfig metrics.Config
	Log           logr.Logger

	// ResourceClient is a gRPC client for the resource service. It is public for testing purposes
	ResourceClient pbresource.ResourceServiceClient
}

// TODO: logs, logs, logs

// Reconcile reads the state of a Kubernetes Pod and reconciles Consul workloads that are 1:1 mapped.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var errs error
	var pod corev1.Pod

	// Ignore the request if the namespace of the pod is not allowed.
	// Strictly speaking, this is not required because the mesh webhook also knows valid namespaces
	// for injection, but it will somewhat reduce the amount of unnecessary deletions for non-injected
	// pods
	if inject.ShouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	rc, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create resource client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	r.ResourceClient = rc

	apiClient, err := consul.NewClientFromConnMgr(r.ConsulClientConfig, r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create Consul API client", "name", req.Name)
		return ctrl.Result{}, err
	}

	if r.ConsulClientConfig.APIClientConfig.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-consul-token", r.ConsulClientConfig.APIClientConfig.Token)
	}

	err = r.Client.Get(ctx, req.NamespacedName, &pod)

	// If the pod object has been deleted (and we get an IsNotFound error),
	// we need to remove the Workload from Consul.
	if k8serrors.IsNotFound(err) {

		// Consul should also clean up the orphaned HealthStatus
		if err := r.deleteWorkload(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		// Delete destinations, if any exist
		if err := r.deleteDestinations(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		if err := r.deleteProxyConfiguration(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		if r.AuthMethod != "" {
			r.Log.Info("deleting ACL tokens for pod", "name", req.Name, "ns", req.Namespace)
			err := r.deleteACLTokensForPod(apiClient, req.NamespacedName)
			if err != nil {
				r.Log.Error(err, "failed to delete ACL tokens for pod", "name", req.Name, "ns", req.Namespace)
				errs = multierror.Append(errs, err)
			}
		}

		return ctrl.Result{}, errs
	} else if err != nil {
		r.Log.Error(err, "failed to get Pod", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved", "name", pod.Name, "ns", pod.Namespace)

	if inject.HasBeenMeshInjected(pod) || inject.IsGateway(pod) {

		// It is possible the pod was scheduled but doesn't have an allocated IP yet, so safely requeue
		if pod.Status.PodIP == "" {
			r.Log.Info("pod does not have IP allocated; re-queueing request", "pod", req.Name, "ns", req.Namespace)
			return ctrl.Result{Requeue: true}, nil
		}

		if err := r.writeProxyConfiguration(ctx, pod); err != nil {
			// We could be racing with the namespace controller.
			// Requeue (which includes backoff) to try again.
			if inject.ConsulNamespaceIsNotFound(err) {
				r.Log.Info("Consul namespace not found; re-queueing request",
					"pod", req.Name, "ns", req.Namespace, "consul-ns",
					r.getConsulNamespace(req.Namespace), "err", err.Error())
				return ctrl.Result{Requeue: true}, nil
			}
			errs = multierror.Append(errs, err)
		}

		if err := r.writeWorkload(ctx, pod); err != nil {
			// Technically this is not needed, but keeping in case this gets refactored in
			// a different order
			if inject.ConsulNamespaceIsNotFound(err) {
				r.Log.Info("Consul namespace not found; re-queueing request",
					"pod", req.Name, "ns", req.Namespace, "consul-ns",
					r.getConsulNamespace(req.Namespace), "err", err.Error())
				return ctrl.Result{Requeue: true}, nil
			}
			errs = multierror.Append(errs, err)
		}

		// Create explicit destinations (if any exist)
		if err := r.writeDestinations(ctx, pod); err != nil {
			// Technically this is not needed, but keeping in case this gets refactored in
			// a different order
			if inject.ConsulNamespaceIsNotFound(err) {
				r.Log.Info("Consul namespace not found; re-queueing request",
					"pod", req.Name, "ns", req.Namespace, "consul-ns",
					r.getConsulNamespace(req.Namespace), "err", err.Error())
				return ctrl.Result{Requeue: true}, nil
			}
			errs = multierror.Append(errs, err)
		}

		if err := r.writeHealthStatus(ctx, pod); err != nil {
			// Technically this is not needed, but keeping in case this gets refactored in
			// a different order
			if inject.ConsulNamespaceIsNotFound(err) {
				r.Log.Info("Consul namespace not found; re-queueing request",
					"pod", req.Name, "ns", req.Namespace, "consul-ns",
					r.getConsulNamespace(req.Namespace), "err", err.Error())
				return ctrl.Result{Requeue: true}, nil
			}
			errs = multierror.Append(errs, err)
		}
	}

	return ctrl.Result{}, errs
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

func (r *Controller) deleteWorkload(ctx context.Context, pod types.NamespacedName) error {
	req := &pbresource.DeleteRequest{
		Id: getWorkloadID(pod.Name, r.getConsulNamespace(pod.Namespace), r.getPartition()),
	}

	_, err := r.ResourceClient.Delete(ctx, req)
	return err
}

func (r *Controller) deleteProxyConfiguration(ctx context.Context, pod types.NamespacedName) error {
	req := &pbresource.DeleteRequest{
		Id: getProxyConfigurationID(pod.Name, r.getConsulNamespace(pod.Namespace), r.getPartition()),
	}

	_, err := r.ResourceClient.Delete(ctx, req)
	return err
}

// deleteACLTokensForPod finds the ACL tokens that belongs to the pod and delete them from Consul.
// It will only check for ACL tokens that have been created with the auth method this controller
// has been configured with and will only delete tokens for the provided pod Name.
func (r *Controller) deleteACLTokensForPod(apiClient *api.Client, pod types.NamespacedName) error {
	// Skip if name is empty.
	if pod.Name == "" {
		return nil
	}

	// Use the V1 logic for getting a compatible namespace
	consulNamespace := namespaces.ConsulNamespace(
		pod.Namespace,
		r.EnableConsulNamespaces,
		r.ConsulDestinationNamespace, r.EnableNSMirroring, r.NSMirroringPrefix,
	)

	// TODO: create an index for the workloadidentity in Consul, which will also require
	// the identity to be attached to the token for templated-policies.
	tokens, _, err := apiClient.ACL().TokenListFiltered(
		api.ACLTokenFilterOptions{
			AuthMethod: r.AuthMethod,
		},
		&api.QueryOptions{
			Namespace: consulNamespace,
		})
	if err != nil {
		return fmt.Errorf("failed to get a list of tokens from Consul: %s", err)
	}

	// We iterate through each token in the auth method, which is terribly inefficient.
	// See discussion above about optimizing the token list query.
	for _, token := range tokens {
		tokenMeta, err := getTokenMetaFromDescription(token.Description)
		// It is possible this is from another component, so continue searching
		if errors.Is(err, NoMetadataErr) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to parse token metadata: %s", err)
		}

		tokenPodName := strings.TrimPrefix(tokenMeta[tokenMetaPodNameKey], pod.Namespace+"/")

		// If we can't find token's pod, delete it.
		if tokenPodName == pod.Name {
			r.Log.Info("deleting ACL token", "name", pod.Name, "namespace", pod.Namespace, "ID", token.AccessorID)
			if _, err := apiClient.ACL().TokenDelete(token.AccessorID, &api.WriteOptions{Namespace: consulNamespace}); err != nil {
				return fmt.Errorf("failed to delete token from Consul: %s", err)
			}
		}
	}
	return nil
}

var NoMetadataErr = fmt.Errorf("failed to extract token metadata from description")

// getTokenMetaFromDescription parses JSON metadata from token's description.
func getTokenMetaFromDescription(description string) (map[string]string, error) {
	re := regexp.MustCompile(`.*({.+})`)

	matches := re.FindStringSubmatch(description)
	if len(matches) != 2 {
		return nil, NoMetadataErr
	}
	tokenMetaJSON := matches[1]

	var tokenMeta map[string]string
	err := json.Unmarshal([]byte(tokenMetaJSON), &tokenMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal token metadata '%s': %s", tokenMetaJSON, err)
	}

	return tokenMeta, nil
}

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
		// Adding a node does not currently work because the node doesn't exist so its health status will always be
		// unhealthy, causing any endpoints on that node to also be unhealthy.
		// TODO: (v2/nitya) Bring this back when node controller is built.
		//NodeName: inject.ConsulNodeNameFromK8sNode(pod.Spec.NodeName),
		Ports: workloadPorts,
	}
	data := inject.ToProtoAny(workload)

	resourceID := getWorkloadID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition())
	r.Log.Info("registering workload with Consul", getLogFieldsForResource(resourceID)...)
	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:       resourceID,
			Metadata: metaFromPod(pod),
			Data:     data,
		},
	}
	_, err := r.ResourceClient.Write(ctx, req)
	return err
}

func (r *Controller) writeProxyConfiguration(ctx context.Context, pod corev1.Pod) error {
	mode, err := r.getTproxyMode(ctx, pod)
	if err != nil {
		return fmt.Errorf("failed to get transparent proxy mode: %w", err)
	}

	exposeConfig, err := r.getExposeConfig(pod)
	if err != nil {
		return fmt.Errorf("failed to get expose config: %w", err)
	}

	bootstrapConfig, err := r.getBootstrapConfig(pod)
	if err != nil {
		return fmt.Errorf("failed to get bootstrap config: %w", err)
	}

	if exposeConfig == nil &&
		bootstrapConfig == nil &&
		mode == pbmesh.ProxyMode_PROXY_MODE_DEFAULT {
		// It's possible to remove interesting annotations and need to clear any existing config,
		// but for now we treat pods as immutable configs owned by other managers.
		return nil
	}

	pc := &pbmesh.ProxyConfiguration{
		Workloads: &pbcatalog.WorkloadSelector{
			Names: []string{pod.GetName()},
		},
		DynamicConfig: &pbmesh.DynamicConfig{
			Mode:         mode,
			ExposeConfig: exposeConfig,
		},
		BootstrapConfig: bootstrapConfig,
	}
	data := inject.ToProtoAny(pc)

	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:       getProxyConfigurationID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()),
			Metadata: metaFromPod(pod),
			Data:     data,
		},
	}
	_, err = r.ResourceClient.Write(ctx, req)
	return err
}

func (r *Controller) getTproxyMode(ctx context.Context, pod corev1.Pod) (pbmesh.ProxyMode, error) {
	// A user can enable/disable tproxy for an entire namespace.
	var ns corev1.Namespace
	err := r.Client.Get(ctx, types.NamespacedName{Name: pod.GetNamespace()}, &ns)
	if err != nil {
		return pbmesh.ProxyMode_PROXY_MODE_DEFAULT, fmt.Errorf("could not get namespace info for %s: %w", pod.GetNamespace(), err)
	}

	tproxyEnabled, err := inject.TransparentProxyEnabled(ns, pod, r.EnableTransparentProxy)
	if err != nil {
		return pbmesh.ProxyMode_PROXY_MODE_DEFAULT, fmt.Errorf("could not determine if transparent proxy is enabled: %w", err)
	}

	if tproxyEnabled {
		return pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT, nil
	}
	return pbmesh.ProxyMode_PROXY_MODE_DEFAULT, nil
}

func (r *Controller) getExposeConfig(pod corev1.Pod) (*pbmesh.ExposeConfig, error) {
	// Expose k8s probes as Envoy listeners if needed.
	overwriteProbes, err := inject.ShouldOverwriteProbes(pod, r.TProxyOverwriteProbes)
	if err != nil {
		return nil, fmt.Errorf("could not determine if probes should be overwritten: %w", err)
	}

	if !overwriteProbes {
		return nil, nil
	}

	var originalPod corev1.Pod
	err = json.Unmarshal([]byte(pod.Annotations[constants.AnnotationOriginalPod]), &originalPod)
	if err != nil {
		return nil, fmt.Errorf("failed to get original pod spec: %w", err)
	}

	exposeConfig := &pbmesh.ExposeConfig{}
	for _, mutatedContainer := range pod.Spec.Containers {
		for _, originalContainer := range originalPod.Spec.Containers {
			if originalContainer.Name == mutatedContainer.Name {
				paths, err := getContainerExposePaths(originalPod, originalContainer, mutatedContainer)
				if err != nil {
					return nil, fmt.Errorf("error getting container expose path for %s: %w", originalContainer.Name, err)
				}

				exposeConfig.ExposePaths = append(exposeConfig.ExposePaths, paths...)
			}
		}
	}

	if len(exposeConfig.ExposePaths) == 0 {
		return nil, nil
	}
	return exposeConfig, nil
}

func getContainerExposePaths(originalPod corev1.Pod, originalContainer, mutatedContainer corev1.Container) ([]*pbmesh.ExposePath, error) {
	var paths []*pbmesh.ExposePath
	if mutatedContainer.LivenessProbe != nil && mutatedContainer.LivenessProbe.HTTPGet != nil {
		originalLivenessPort, err := inject.PortValueFromIntOrString(originalPod, originalContainer.LivenessProbe.HTTPGet.Port)
		if err != nil {
			return nil, err
		}

		newPath := &pbmesh.ExposePath{
			ListenerPort:  uint32(mutatedContainer.LivenessProbe.HTTPGet.Port.IntValue()),
			LocalPathPort: originalLivenessPort,
			Path:          mutatedContainer.LivenessProbe.HTTPGet.Path,
		}
		paths = append(paths, newPath)
	}
	if mutatedContainer.ReadinessProbe != nil && mutatedContainer.ReadinessProbe.HTTPGet != nil {
		originalReadinessPort, err := inject.PortValueFromIntOrString(originalPod, originalContainer.ReadinessProbe.HTTPGet.Port)
		if err != nil {
			return nil, err
		}

		newPath := &pbmesh.ExposePath{
			ListenerPort:  uint32(mutatedContainer.ReadinessProbe.HTTPGet.Port.IntValue()),
			LocalPathPort: originalReadinessPort,
			Path:          mutatedContainer.ReadinessProbe.HTTPGet.Path,
		}
		paths = append(paths, newPath)
	}
	if mutatedContainer.StartupProbe != nil && mutatedContainer.StartupProbe.HTTPGet != nil {
		originalStartupPort, err := inject.PortValueFromIntOrString(originalPod, originalContainer.StartupProbe.HTTPGet.Port)
		if err != nil {
			return nil, err
		}

		newPath := &pbmesh.ExposePath{
			ListenerPort:  uint32(mutatedContainer.StartupProbe.HTTPGet.Port.IntValue()),
			LocalPathPort: originalStartupPort,
			Path:          mutatedContainer.StartupProbe.HTTPGet.Path,
		}
		paths = append(paths, newPath)
	}
	return paths, nil
}

func (r *Controller) getBootstrapConfig(pod corev1.Pod) (*pbmesh.BootstrapConfig, error) {
	bootstrap := &pbmesh.BootstrapConfig{}

	// If metrics are enabled, the BootstrapConfig should set envoy_prometheus_bind_addr to a listener on 0.0.0.0 on
	// the PrometheusScrapePort. The backend for this listener will be determined by
	// the consul-dataplane command line flags generated by the webhook.
	// If there is a merged metrics server, the backend would be that server.
	// If we are not running the merged metrics server, the backend should just be the Envoy metrics endpoint.
	enableMetrics, err := r.MetricsConfig.EnableMetrics(pod)
	if err != nil {
		return nil, fmt.Errorf("error determining if metrics are enabled: %w", err)
	}
	if enableMetrics {
		prometheusScrapePort, err := r.MetricsConfig.PrometheusScrapePort(pod)
		if err != nil {
			return nil, err
		}
		prometheusScrapeListener := fmt.Sprintf("0.0.0.0:%s", prometheusScrapePort)
		bootstrap.PrometheusBindAddr = prometheusScrapeListener
	}

	if r.EnableTelemetryCollector {
		bootstrap.TelemetryCollectorBindSocketDir = DefaultTelemetryBindSocketDir
	}

	if proto.Equal(bootstrap, &pbmesh.BootstrapConfig{}) {
		return nil, nil
	}
	return bootstrap, nil
}

func (r *Controller) writeHealthStatus(ctx context.Context, pod corev1.Pod) error {
	status := getHealthStatusFromPod(pod)

	hs := &pbcatalog.HealthStatus{
		Type:        constants.ConsulKubernetesCheckType,
		Status:      status,
		Description: constants.ConsulKubernetesCheckName,
		Output:      getHealthStatusReason(status, pod),
	}
	data := inject.ToProtoAny(hs)

	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:       getHealthStatusID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()),
			Owner:    getWorkloadID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()),
			Metadata: metaFromPod(pod),
			Data:     data,
		},
	}
	_, err := r.ResourceClient.Write(ctx, req)
	return err
}

// TODO: delete ACL token for workload
// deleteACLTokensForServiceInstance finds the ACL tokens that belongs to the service instance and deletes it from Consul.
// It will only check for ACL tokens that have been created with the auth method this controller
// has been configured with and will only delete tokens for the provided podName.
// func (r *Controller) deleteACLTokensForWorkload(apiClient *api.Client, svc *api.AgentService, k8sNS, podName string) error {

// writeDestinations will write explicit destinations if pod annotations exist.
func (r *Controller) writeDestinations(ctx context.Context, pod corev1.Pod) error {
	uss, err := inject.ProcessPodDestinations(pod, r.EnableConsulPartitions, r.EnableConsulNamespaces)
	if err != nil {
		return fmt.Errorf("error processing destination annotations: %s", err.Error())
	}
	if uss == nil {
		return nil
	}

	data := inject.ToProtoAny(uss)
	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:       getDestinationsID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()),
			Metadata: metaFromPod(pod),
			Data:     data,
		},
	}
	_, err = r.ResourceClient.Write(ctx, req)

	return err
}

func (r *Controller) deleteDestinations(ctx context.Context, pod types.NamespacedName) error {
	req := &pbresource.DeleteRequest{
		Id: getDestinationsID(pod.Name, r.getConsulNamespace(pod.Namespace), r.getPartition()),
	}

	_, err := r.ResourceClient.Delete(ctx, req)
	return err
}

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
			name := inject.WorkloadPortName(&port)

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

func metaFromPod(pod corev1.Pod) map[string]string {
	// TODO: allow custom workload metadata
	meta := map[string]string{
		constants.MetaKeyKubeNS:    pod.GetNamespace(),
		constants.MetaKeyManagedBy: constants.ManagedByPodValue,
	}

	if gatewayKind := pod.Annotations[constants.AnnotationGatewayKind]; gatewayKind != "" {
		meta[constants.MetaGatewayKind] = gatewayKind
	}

	return meta
}

// getHealthStatusFromPod checks the Pod for a "Ready" condition that is true.
// This is true when all the containers are ready, vs. "Running" on the PodPhase,
// which is true if any container is running.
func getHealthStatusFromPod(pod corev1.Pod) pbcatalog.Health {
	if pod.Status.Conditions == nil {
		return pbcatalog.Health_HEALTH_CRITICAL
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return pbcatalog.Health_HEALTH_PASSING
		}
	}

	return pbcatalog.Health_HEALTH_CRITICAL
}

// getHealthStatusReason takes Consul's health check status (either passing or critical)
// and the pod to return a descriptive output for the HealthStatus Output.
func getHealthStatusReason(state pbcatalog.Health, pod corev1.Pod) string {
	if state == pbcatalog.Health_HEALTH_PASSING {
		return constants.KubernetesSuccessReasonMsg
	}

	return fmt.Sprintf("Pod \"%s/%s\" is not ready", pod.GetNamespace(), pod.GetName())
}

func getWorkloadID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbcatalog.WorkloadType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func getProxyConfigurationID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbmesh.ProxyConfigurationType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func getHealthStatusID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbcatalog.HealthStatusType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func getDestinationsID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbmesh.DestinationsType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func getLogFieldsForResource(id *pbresource.ID) []any {
	return []any{
		"name", id.Name,
		"ns", id.Tenancy.Namespace,
		"partition", id.Tenancy.Partition,
	}
}
