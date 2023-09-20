// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package pod

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v1alpha1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/proto"
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
	DefaultTelemetryBindSocketDir = "/consul/mesh-inject"
	consulNodeAddress             = "127.0.0.1"
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

		// Delete upstreams, if any exist
		if err := r.deleteUpstreams(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		if err := r.deleteProxyConfiguration(ctx, req.NamespacedName); err != nil {
			errs = multierror.Append(errs, err)
		}

		return ctrl.Result{}, errs
	} else if err != nil {
		r.Log.Error(err, "failed to get Pod", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved", "name", pod.Name, "ns", pod.Namespace)

	if common.HasBeenMeshInjected(pod) {
		if err := r.writeProxyConfiguration(ctx, pod); err != nil {
			// We could be racing with the namespace controller.
			// Requeue (which includes backoff) to try again.
			if common.ConsulNamespaceIsNotFound(err) {
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
			if common.ConsulNamespaceIsNotFound(err) {
				r.Log.Info("Consul namespace not found; re-queueing request",
					"pod", req.Name, "ns", req.Namespace, "consul-ns",
					r.getConsulNamespace(req.Namespace), "err", err.Error())
				return ctrl.Result{Requeue: true}, nil
			}
			errs = multierror.Append(errs, err)
		}

		// Create explicit upstreams (if any exist)
		if err := r.writeUpstreams(ctx, pod); err != nil {
			// Technically this is not needed, but keeping in case this gets refactored in
			// a different order
			if common.ConsulNamespaceIsNotFound(err) {
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
			if common.ConsulNamespaceIsNotFound(err) {
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
		//NodeName: common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName),
		Ports: workloadPorts,
	}
	data := common.ToProtoAny(workload)

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

// writeUpstreams will write explicit upstreams if pod annotations exist.
func (r *Controller) writeUpstreams(ctx context.Context, pod corev1.Pod) error {
	uss, err := r.processUpstreams(pod)
	if err != nil {
		return fmt.Errorf("error processing upstream annotations: %s", err.Error())
	}
	if uss == nil {
		return nil
	}

	data := common.ToProtoAny(uss)
	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:       getUpstreamsID(pod.GetName(), r.getConsulNamespace(pod.Namespace), r.getPartition()),
			Metadata: metaFromPod(pod),
			Data:     data,
		},
	}
	_, err = r.ResourceClient.Write(ctx, req)

	return err
}

func (r *Controller) deleteUpstreams(ctx context.Context, pod types.NamespacedName) error {
	req := &pbresource.DeleteRequest{
		Id: getUpstreamsID(pod.Name, r.getConsulNamespace(pod.Namespace), r.getPartition()),
	}

	_, err := r.ResourceClient.Delete(ctx, req)
	return err
}

// processUpstreams reads the list of upstreams from the Pod annotation and converts them into a list of pbmesh.Upstreams
// objects.
func (r *Controller) processUpstreams(pod corev1.Pod) (*pbmesh.Upstreams, error) {
	upstreams := &pbmesh.Upstreams{}
	raw, ok := pod.Annotations[constants.AnnotationMeshDestinations]
	if !ok || raw == "" {
		return nil, nil
	}

	upstreams.Workloads = &pbcatalog.WorkloadSelector{
		Names: []string{pod.Name},
	}

	for _, raw := range strings.Split(raw, ",") {
		var upstream *pbmesh.Upstream

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
			upstream, err = r.processLabeledUpstream(pod, raw)
			if err != nil {
				return &pbmesh.Upstreams{}, err
			}
		} else {
			var err error
			upstream, err = r.processUnlabeledUpstream(pod, raw)
			if err != nil {
				return &pbmesh.Upstreams{}, err
			}
		}

		upstreams.Upstreams = append(upstreams.Upstreams, upstream)
	}

	return upstreams, nil
}

// processLabeledUpstream processes an upstream in the format:
// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-peer].peer:[port]
// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-partition].ap:[port]
// [service-port-name].port.[service-name].svc.[service-namespace].ns.[service-datacenter].dc:[port].
// peer/ap/dc are mutually exclusive. At minimum service-port-name and service-name are required.
// The ordering matters for labeled as well as unlabeled. The ordering of the labeled parameters should follow
// the order and requirements of the unlabeled parameters.
// TODO: enable dc and peer support when ready, currently return errors if set.
func (r *Controller) processLabeledUpstream(pod corev1.Pod, rawUpstream string) (*pbmesh.Upstream, error) {
	parts := strings.SplitN(rawUpstream, ":", 3)
	var port int32
	port, _ = common.PortValue(pod, strings.TrimSpace(parts[1]))
	if port <= 0 {
		return &pbmesh.Upstream{}, fmt.Errorf("port value %d in upstream is invalid: %s", port, rawUpstream)
	}

	service := parts[0]
	pieces := strings.Split(service, ".")

	var portName, datacenter, svcName, namespace, partition, peer string
	if r.EnableConsulNamespaces || r.EnableConsulPartitions {
		switch len(pieces) {
		case 8:
			end := strings.TrimSpace(pieces[7])
			switch end {
			case "peer":
				// TODO: uncomment and remove error when peers supported
				//peer = strings.TrimSpace(pieces[6])
				return &pbmesh.Upstream{}, fmt.Errorf("upstream currently does not support peers: %s", rawUpstream)
			case "ap":
				partition = strings.TrimSpace(pieces[6])
			case "dc":
				// TODO: uncomment and remove error when datacenters are supported
				//datacenter = strings.TrimSpace(pieces[6])
				return &pbmesh.Upstream{}, fmt.Errorf("upstream currently does not support datacenters: %s", rawUpstream)
			default:
				return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			fallthrough
		case 6:
			if strings.TrimSpace(pieces[5]) == "ns" {
				namespace = strings.TrimSpace(pieces[4])
			} else {
				return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			fallthrough
		case 4:
			if strings.TrimSpace(pieces[3]) == "svc" {
				svcName = strings.TrimSpace(pieces[2])
			} else {
				return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			if strings.TrimSpace(pieces[1]) == "port" {
				portName = strings.TrimSpace(pieces[0])
			} else {
				return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
		default:
			return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
		}
	} else {
		switch len(pieces) {
		case 6:
			end := strings.TrimSpace(pieces[5])
			switch end {
			case "peer":
				// TODO: uncomment and remove error when peers supported
				//peer = strings.TrimSpace(pieces[4])
				return &pbmesh.Upstream{}, fmt.Errorf("upstream currently does not support peers: %s", rawUpstream)
			case "dc":
				// TODO: uncomment and remove error when datacenter supported
				//datacenter = strings.TrimSpace(pieces[4])
				return &pbmesh.Upstream{}, fmt.Errorf("upstream currently does not support datacenters: %s", rawUpstream)
			default:
				return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			// TODO: uncomment and remove error when datacenter and/or peers supported
			//fallthrough
		case 4:
			if strings.TrimSpace(pieces[3]) == "svc" {
				svcName = strings.TrimSpace(pieces[2])
			} else {
				return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			if strings.TrimSpace(pieces[1]) == "port" {
				portName = strings.TrimSpace(pieces[0])
			} else {
				return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
		default:
			return &pbmesh.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
		}
	}

	upstream := pbmesh.Upstream{
		DestinationRef: &pbresource.Reference{
			Type: upstreamReferenceType(),
			Tenancy: &pbresource.Tenancy{
				Partition: getDefaultConsulPartition(partition),
				Namespace: getDefaultConsulNamespace(namespace),
				PeerName:  getDefaultConsulPeer(peer),
			},
			Name: svcName,
		},
		DestinationPort: portName,
		Datacenter:      datacenter,
		ListenAddr: &pbmesh.Upstream_IpPort{
			IpPort: &pbmesh.IPPortAddress{
				Port: uint32(port),
				Ip:   consulNodeAddress,
			},
		},
	}

	return &upstream, nil
}

// processUnlabeledUpstream processes an upstream in the format:
// [service-port-name].[service-name].[service-namespace].[service-partition]:[port]:[optional datacenter].
// There is no unlabeled field for peering.
// TODO: enable dc and peer support when ready, currently return errors if set. We also most likely won't need to return an error at all.
func (r *Controller) processUnlabeledUpstream(pod corev1.Pod, rawUpstream string) (*pbmesh.Upstream, error) {
	var portName, datacenter, svcName, namespace, partition string
	var port int32
	var upstream pbmesh.Upstream

	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = common.PortValue(pod, strings.TrimSpace(parts[1]))

	// If Consul Namespaces or Admin Partitions are enabled, attempt to parse the
	// upstream for a namespace.
	if r.EnableConsulNamespaces || r.EnableConsulPartitions {
		pieces := strings.SplitN(parts[0], ".", 4)
		switch len(pieces) {
		case 4:
			partition = strings.TrimSpace(pieces[3])
			fallthrough
		case 3:
			namespace = strings.TrimSpace(pieces[2])
			fallthrough
		default:
			svcName = strings.TrimSpace(pieces[1])
			portName = strings.TrimSpace(pieces[0])
		}
	} else {
		pieces := strings.SplitN(parts[0], ".", 2)
		svcName = strings.TrimSpace(pieces[1])
		portName = strings.TrimSpace(pieces[0])
	}

	// parse the optional datacenter
	if len(parts) > 2 {
		// TODO: uncomment and remove error when datacenters supported
		//datacenter = strings.TrimSpace(parts[2])
		return &pbmesh.Upstream{}, fmt.Errorf("upstream currently does not support datacenters: %s", rawUpstream)
	}

	if port > 0 {
		upstream = pbmesh.Upstream{
			DestinationRef: &pbresource.Reference{
				Type: upstreamReferenceType(),
				Tenancy: &pbresource.Tenancy{
					Partition: getDefaultConsulPartition(partition),
					Namespace: getDefaultConsulNamespace(namespace),
					PeerName:  getDefaultConsulPeer(""),
				},
				Name: svcName,
			},
			DestinationPort: portName,
			Datacenter:      datacenter,
			ListenAddr: &pbmesh.Upstream_IpPort{
				IpPort: &pbmesh.IPPortAddress{
					Port: uint32(port),
					Ip:   consulNodeAddress,
				},
			},
		}
	}
	return &upstream, nil
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
		constants.MetaKeyKubeNS:    pod.GetNamespace(),
		constants.MetaKeyManagedBy: constants.ManagedByPodValue,
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

func getUpstreamsID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: &pbresource.Type{
			Group:        "mesh",
			GroupVersion: "v1alpha1",
			Kind:         "Upstreams",
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

func upstreamReferenceType() *pbresource.Type {
	return &pbresource.Type{
		Group:        "catalog",
		GroupVersion: "v1alpha1",
		Kind:         "Service",
	}
}

func getDefaultConsulNamespace(ns string) string {
	if ns == "" {
		ns = constants.DefaultConsulNS
	}

	return ns
}

func getDefaultConsulPartition(ap string) string {
	if ap == "" {
		ap = constants.DefaultConsulPartition
	}

	return ap
}

func getDefaultConsulPeer(peer string) string {
	if peer == "" {
		peer = constants.DefaultConsulPeer
	}

	return peer
}
