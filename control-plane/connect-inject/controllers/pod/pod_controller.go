// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package pod

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v1alpha1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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
	metaKeyManagedBy = "managed-by"

	DefaultTelemetryBindSocketDir = "/consul/mesh-inject"
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

	// EnableTransparentProxy controls whether transparent proxy should be enabled
	// for all proxy service registrations.
	EnableTransparentProxy bool
	// TProxyOverwriteProbes controls whether the pods controller should expose pod's HTTP probes
	// via Envoy proxy.
	TProxyOverwriteProbes bool

	// AuthMethod is the name of the Kubernetes Auth Method that
	// was used to login with Consul. The Endpoints controller
	// will delete any tokens associated with this auth method
	// whenever service instances are deregistered.
	AuthMethod string

	// EnableTelemetryCollector controls whether the proxy service should be registered
	// with config to enable telemetry forwarding.
	EnableTelemetryCollector bool

	MetricsConfig metrics.Config
	Log           logr.Logger

	Scheme *runtime.Scheme
	context.Context

	// ResourceClient is a gRPC client for the resource service. It is public for testing purposes
	ResourceClient pbresource.ResourceServiceClient
}

// TODO: logs, logs, logs

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

		// Consul should also clean up the orphaned HealthStatus
		if err := r.deleteWorkload(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		// TODO: clean up ACL Tokens

		// TODO: delete explicit upstreams
		//if err := r.deleteUpstreams(ctx, pod); err != nil {
		//	errs = multierror.Append(errs, err)
		//}

		if err := r.deleteProxyConfiguration(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		return ctrl.Result{}, errs
	} else if err != nil {
		r.Log.Error(err, "failed to get Pod", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved", "name", pod.Name, "ns", pod.Namespace)

	if hasBeenInjected(pod) {
		if err := r.writeProxyConfiguration(ctx, pod); err != nil {
			errs = multierror.Append(errs, err)
		}

		if err := r.writeWorkload(ctx, pod); err != nil {
			errs = multierror.Append(errs, err)
		}

		// TODO: create explicit upstreams
		//if err := r.writeUpstreams(ctx, pod); err != nil {
		//	errs = multierror.Append(errs, err)
		//}

		if err := r.writeHealthStatus(ctx, pod); err != nil {
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

func (r *Controller) deleteProxyConfiguration(ctx context.Context, pod types.NamespacedName) error {
	req := &pbresource.DeleteRequest{
		Id: getProxyConfigurationID(pod.Name, r.getConsulNamespace(pod.Namespace), r.getPartition()),
	}

	_, err := r.ResourceClient.Delete(ctx, req)
	return err
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
		NodeName: common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName),
		Ports:    workloadPorts,
	}
	data := common.ToProtoAny(workload)

	r.Log.Info("****Trying to write the following workload", "workload", workload, "id", getWorkloadID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()))

	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:       getWorkloadID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()),
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
	data := common.ToProtoAny(pc)

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

	tproxyEnabled, err := common.TransparentProxyEnabled(ns, pod, r.EnableTransparentProxy)
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
	overwriteProbes, err := common.ShouldOverwriteProbes(pod, r.TProxyOverwriteProbes)
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
		originalLivenessPort, err := common.PortValueFromIntOrString(originalPod, originalContainer.LivenessProbe.HTTPGet.Port)
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
		originalReadinessPort, err := common.PortValueFromIntOrString(originalPod, originalContainer.ReadinessProbe.HTTPGet.Port)
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
		originalStartupPort, err := common.PortValueFromIntOrString(originalPod, originalContainer.StartupProbe.HTTPGet.Port)
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
	// the PrometheusScrapePort that points to a metrics backend. The backend for this listener will be determined by
	// the envoy bootstrapping command (consul connect envoy) or the consul-dataplane GetBoostrapParams rpc.
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
	data := common.ToProtoAny(hs)

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

// TODO: add support for explicit upstreams
//func (r *Controller) writeUpstreams(pod corev1.Pod) error

//func (r *Controller) deleteUpstreams(pod corev1.Pod) error

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

func metaFromPod(pod corev1.Pod) map[string]string {
	// TODO: allow custom workload metadata
	return map[string]string{
		constants.MetaKeyKubeNS: pod.GetNamespace(),
		metaKeyManagedBy:        constants.ManagedByPodValue,
	}
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
		Type: &pbresource.Type{
			Group:        "catalog",
			GroupVersion: "v1alpha1",
			Kind:         "Workload",
		},
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
	}
}

func getProxyConfigurationID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: &pbresource.Type{
			Group:        "mesh",
			GroupVersion: "v1alpha1",
			Kind:         "ProxyConfiguration",
		},
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
	}
}

func getHealthStatusID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: &pbresource.Type{
			Group:        "catalog",
			GroupVersion: "v1alpha1",
			Kind:         "HealthStatus",
		},
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
	}
}
