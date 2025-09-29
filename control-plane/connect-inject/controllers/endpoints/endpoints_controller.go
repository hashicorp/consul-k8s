// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/parsetags"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	metaKeyKubeServiceName = "k8s-service-name"

	metaKeyManagedBy           = "managed-by"
	metaKeySyntheticNode       = "synthetic-node"
	metaKeyConsulWANFederation = "consul-wan-federation"
	tokenMetaPodNameKey        = "pod"

	// Gateway types for registration.
	meshGateway        = "mesh-gateway"
	terminatingGateway = "terminating-gateway"
	ingressGateway     = "ingress-gateway"
	apiGateway         = "api-gateway"

	envoyPrometheusBindAddr              = "envoy_prometheus_bind_addr"
	envoyTelemetryCollectorBindSocketDir = "envoy_telemetry_collector_bind_socket_dir"
	defaultNS                            = "default"

	// clusterIPTaggedAddressName is the key for the tagged address to store the service's cluster IP and service port
	// in Consul. Note: This value should not be changed without a corresponding change in Consul.
	clusterIPTaggedAddressName = "virtual"

	// consulNodeAddress is the address of the consul node (defined by ConsulNodeName).
	// This address does not need to be routable as this node is ephemeral, and we're only providing it because
	// Consul's API currently requires node address to be provided when registering a node.
	consulNodeAddress = "127.0.0.1"
)

type Controller struct {
	client.Client
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	// Only endpoints in the AllowK8sNamespacesSet are reconciled.
	AllowK8sNamespacesSet mapset.Set
	// Endpoints in the DenyK8sNamespacesSet are ignored.
	DenyK8sNamespacesSet mapset.Set
	// EnableConsulPartitions indicates that a user is running Consul Enterprise
	// with version 1.11+ which supports Admin Partitions.
	EnableConsulPartitions bool
	// EnableConsulNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which supports namespaces.
	EnableConsulNamespaces bool
	// EnableWANFederation indicates that a user is running Consul with
	// WAN Federation enabled.
	EnableWANFederation bool
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
	// CrossNSACLPolicy is the name of the ACL policy to attach to
	// any created Consul namespaces to allow cross namespace service discovery.
	// Only necessary if ACLs are enabled.
	CrossNSACLPolicy string
	// Lifecycle config set graceful startup/shutdown defaults for pods.
	LifecycleConfig lifecycle.Config
	// ReleaseName is the Consul Helm installation release.
	ReleaseName string
	// ReleaseNamespace is the namespace where Consul is installed.
	ReleaseNamespace string
	// EnableTransparentProxy controls whether transparent proxy should be enabled
	// for all proxy service registrations.
	EnableTransparentProxy bool
	// TProxyOverwriteProbes controls whether the endpoints controller should expose pod's HTTP probes
	// via Envoy proxy.
	TProxyOverwriteProbes bool
	// AuthMethod is the name of the Kubernetes Auth Method that
	// was used to login with Consul. The Endpoints controller
	// will delete any tokens associated with this auth method
	// whenever service instances are deregistered.
	AuthMethod string

	// EnableAutoEncrypt indicates whether we should use auto-encrypt when talking
	// to Consul client agents.
	EnableAutoEncrypt bool

	// EnableTelemetryCollector controls whether the proxy service should be registered
	// with config to enable telemetry forwarding.
	EnableTelemetryCollector bool

	MetricsConfig metrics.Config
	Log           logr.Logger

	Scheme *runtime.Scheme
	context.Context

	// consulClientHttpPort is only used in tests.
	consulClientHttpPort int
	NodeMeta             map[string]string
}

// Reconcile reads the state of an Endpoints object for a Kubernetes Service and reconciles Consul services which
// correspond to the Kubernetes Service. These events are driven by changes to the Pods backing the Kube service.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var errs error
	var serviceEndpoints corev1.Endpoints

	// Ignore the request if the namespace of the endpoint is not allowed.
	if common.ShouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	// Create Consul client for this reconcile.
	serverState, err := r.ConsulServerConnMgr.State()
	if err != nil {
		r.Log.Error(err, "failed to get Consul server state", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	apiClient, err := consul.NewClientFromConnMgrState(r.ConsulClientConfig, serverState)
	if err != nil {
		r.Log.Error(err, "failed to create Consul API client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	err = r.Client.Get(ctx, req.NamespacedName, &serviceEndpoints)

	// If the endpoints object has been deleted (and we get an IsNotFound
	// error), we need to deregister all instances in Consul for that service.
	if k8serrors.IsNotFound(err) {
		// Deregister all instances in Consul for this service. The function deregisterService handles
		// the case where the Consul service name is different from the Kubernetes service name.
		requeueAfter, err := r.deregisterService(ctx, apiClient, req.Name, req.Namespace, nil)
		return ctrl.Result{RequeueAfter: requeueAfter}, err
	} else if err != nil {
		r.Log.Error(err, "failed to get Endpoints", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)

	// If the endpoints object has the label "consul.hashicorp.com/service-ignore" set to true, deregister all instances in Consul for this service.
	// It is possible that the endpoints object has never been registered, in which case deregistration is a no-op.
	if isLabeledIgnore(serviceEndpoints.Labels) {
		// We always deregister the service to handle the case where a user has registered the service, then added the label later.
		r.Log.Info("ignoring endpoint labeled with `consul.hashicorp.com/service-ignore: \"true\"`", "name", req.Name, "namespace", req.Namespace)
		requeueAfter, err := r.deregisterService(ctx, apiClient, req.Name, req.Namespace, nil)
		return ctrl.Result{RequeueAfter: requeueAfter}, err
	}

	// deregisterEndpointAddress stores every IP that corresponds to a Pod in the Endpoints object. It is used to compare
	// against service instances in Consul to deregister them if they are not in the map.
	deregisterEndpointAddress := map[string]bool{}

	// Register all addresses of this Endpoints object as service instances in Consul.
	for _, subset := range serviceEndpoints.Subsets {
		for address, healthStatus := range mapAddresses(subset) {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				var pod corev1.Pod
				objectKey := types.NamespacedName{Name: address.TargetRef.Name, Namespace: address.TargetRef.Namespace}
				if err = r.Client.Get(ctx, objectKey, &pod); err != nil {
					// If the pod doesn't exist anymore, set up the deregisterEndpointAddress map to deregister it.
					if k8serrors.IsNotFound(err) {
						deregisterEndpointAddress[address.IP] = true
						r.Log.Info("pod not found", "name", address.TargetRef.Name)
					} else {
						// If there was a different error fetching the pod, then log the error but don't deregister it
						// since this could be a K8s API blip and we don't want to prematurely deregister.
						deregisterEndpointAddress[address.IP] = false
						r.Log.Error(err, "failed to get pod", "name", address.TargetRef.Name)
						errs = multierror.Append(errs, err)
					}
					continue
				}

				svcName, ok := pod.Annotations[constants.AnnotationKubernetesService]
				if ok && serviceEndpoints.Name != svcName {
					r.Log.Info("ignoring endpoint because it doesn't match explicit service annotation", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
					// Set up the deregisterEndpointAddress to deregister service instances that don't match the annotation.
					deregisterEndpointAddress[address.IP] = true
					continue
				}

				// Skip registration if node information is incomplete to prevent duplicate registrations.
				// Retry for 30 seconds because the pod IP may not be immediately available after pod creation.
				retries := 15
				for range retries {
					if pod.Spec.NodeName == "" || pod.Status.PodIP == "" || pod.Status.HostIP == "" {
						r.Log.Info("waiting for pod to have complete node information before registering",
							"pod", pod.Name, "namespace", pod.Namespace,
							"nodeName", pod.Spec.NodeName, "podIP", pod.Status.PodIP, "hostIP", pod.Status.HostIP)
						time.Sleep(2 * time.Second)
						if err = r.Client.Get(ctx, objectKey, &pod); err != nil {
							// If the pod doesn't exist anymore, set up the deregisterEndpointAddress map to deregister it.
							if k8serrors.IsNotFound(err) {
								deregisterEndpointAddress[address.IP] = true
								r.Log.Info("pod not found", "name", address.TargetRef.Name)
							} else {
								// If there was a different error fetching the pod, then log the error but don't deregister it
								// since this could be a K8s API blip and we don't want to prematurely deregister.
								deregisterEndpointAddress[address.IP] = false
								r.Log.Error(err, "failed to get pod", "name", address.TargetRef.Name)
								errs = multierror.Append(errs, err)
							}
							break
						}
					} else {
						break
					}
				}
				// If after retries the node information is still incomplete, skip registration.
				// Set up the deregisterEndpointAddress map to deregister it.
				// This could happen if the pod is stuck in Pending state for a long time.
				if pod.Spec.NodeName == "" || pod.Status.PodIP == "" || pod.Status.HostIP == "" {
					r.Log.Info("skipping pod registration due to incomplete node information",
						"pod", pod.Name, "namespace", pod.Namespace,
						"nodeName", pod.Spec.NodeName, "podIP", pod.Status.PodIP, "hostIP", pod.Status.HostIP)
					// Don't set up for deregistration since we're not registering it
					continue
				}

				if isTelemetryCollector(pod) {
					if err = r.ensureNamespaceExists(apiClient, pod); err != nil {
						r.Log.Error(err, "failed to ensure a namespace exists for Consul Telemetry Collector")
						errs = multierror.Append(errs, err)
					}
				}

				if hasBeenInjected(pod) {
					if isConsulDataplaneSupported(pod) {
						if err = r.registerServicesAndHealthCheck(apiClient, pod, serviceEndpoints, healthStatus); err != nil {
							r.Log.Error(err, "failed to register services or health check", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
							errs = multierror.Append(errs, err)
						}
						// Build the deregisterEndpointAddress map up for deregistering service instances later.
						deregisterEndpointAddress[pod.Status.PodIP] = false
					} else {
						r.Log.Info("detected an update to pre-consul-dataplane service", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
						nodeAgentClientCfg, err := r.consulClientCfgForNodeAgent(apiClient, pod, serverState)
						if err != nil {
							r.Log.Error(err, "failed to create node-local Consul API client", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
							errs = multierror.Append(errs, err)
							continue
						}
						r.Log.Info("updating health check on the Consul client", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
						if err = r.updateHealthCheckOnConsulClient(nodeAgentClientCfg, pod, serviceEndpoints, healthStatus); err != nil {
							r.Log.Error(err, "failed to update health check on Consul client", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace, "consul-client-ip", pod.Status.HostIP)
							errs = multierror.Append(errs, err)
						}
						// We want to skip the rest of the reconciliation because we only care about updating health checks for existing services
						// in the case when Consul clients are running in the cluster. If endpoints are deleted, consul clients
						// will detect that they are unhealthy, and we don't need to worry about keeping them up-to-date.
						// This is so that health checks are still updated during an upgrade to consul-dataplane.
						continue
					}
				}

				if isGateway(pod) {
					if err = r.registerGateway(apiClient, pod, serviceEndpoints, healthStatus); err != nil {
						r.Log.Error(err, "failed to register gateway or health check", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
						errs = multierror.Append(errs, err)
					}
					// Build the deregisterEndpointAddress map up for deregistering service instances later.
					deregisterEndpointAddress[pod.Status.PodIP] = false
				}
			}
		}
	}

	// Compare service instances in Consul with addresses in Endpoints. If an address is not in Endpoints, deregister
	// from Consul. This uses deregisterEndpointAddress which is populated with the addresses in the Endpoints object to
	// either deregister or keep during the registration codepath.
	requeueAfter, err := r.deregisterService(ctx, apiClient, serviceEndpoints.Name, serviceEndpoints.Namespace, deregisterEndpointAddress)
	if err != nil {
		r.Log.Error(err, "failed to deregister endpoints", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
		errs = multierror.Append(errs, err)
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, errs
}

func (r *Controller) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(r)
}

// registerServicesAndHealthCheck creates Consul registrations for the service and proxy and registers them with Consul.
// It also upserts a Kubernetes health check for the service based on whether the endpoint address is ready.
func (r *Controller) registerServicesAndHealthCheck(apiClient *api.Client, pod corev1.Pod, serviceEndpoints corev1.Endpoints, healthStatus string) error {
	var managedByEndpointsController bool
	if raw, ok := pod.Labels[constants.KeyManagedBy]; ok && raw == constants.ManagedByValue {
		managedByEndpointsController = true
	}
	// For pods managed by this controller, create and register the service instance.
	if managedByEndpointsController {
		// Get information from the pod to create service instance registrations.
		serviceRegistration, proxyServiceRegistration, err := r.createServiceRegistrations(pod, serviceEndpoints, healthStatus)
		if err != nil {
			r.Log.Error(err, "failed to create service registrations for endpoints", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
			return err
		}

		// Register the service instance with Consul.
		r.Log.Info("registering service with Consul", "name", serviceRegistration.Service.Service,
			"id", serviceRegistration.Service.ID, "health", healthStatus)
		_, err = apiClient.Catalog().Register(serviceRegistration, nil)
		if err != nil {
			r.Log.Error(err, "failed to register service", "name", serviceRegistration.Service.Service)
			return err
		}

		// Add manual ip to the VIP table
		r.Log.Info("adding manual ip to virtual ip table in Consul", "name", serviceRegistration.Service.Service,
			"id", serviceRegistration.ID)
		err = assignServiceVirtualIP(r.Context, apiClient, serviceRegistration.Service)
		if err != nil {
			r.Log.Error(err, "failed to add ip to virtual ip table", "name", serviceRegistration.Service.Service)
		}

		// Register the proxy service instance with Consul.
		r.Log.Info("registering proxy service with Consul", "name", proxyServiceRegistration.Service.Service, "id", proxyServiceRegistration.Service.ID, "health", healthStatus)
		_, err = apiClient.Catalog().Register(proxyServiceRegistration, nil)
		if err != nil {
			r.Log.Error(err, "failed to register proxy service", "name", proxyServiceRegistration.Service.Service)
			return err
		}
	}
	return nil
}

func parseLocality(node corev1.Node) *api.Locality {
	region := node.Labels[corev1.LabelTopologyRegion]
	zone := node.Labels[corev1.LabelTopologyZone]

	if region == "" {
		return nil
	}

	return &api.Locality{
		Region: region,
		Zone:   zone,
	}
}

// registerGateway creates Consul registrations for the Connect Gateways and registers them with Consul.
// It also upserts a Kubernetes health check for the service based on whether the endpoint address is ready.
func (r *Controller) registerGateway(apiClient *api.Client, pod corev1.Pod, serviceEndpoints corev1.Endpoints, healthStatus string) error {
	var managedByEndpointsController bool
	if raw, ok := pod.Labels[constants.KeyManagedBy]; ok && raw == constants.ManagedByValue {
		managedByEndpointsController = true
	}
	// For pods managed by this controller, create and register the service instance.
	if managedByEndpointsController {
		// Get information from the pod to create service instance registrations.
		serviceRegistration, err := r.createGatewayRegistrations(pod, serviceEndpoints, healthStatus)
		if err != nil {
			r.Log.Error(err, "failed to create service registrations for endpoints", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
			return err
		}

		if r.EnableConsulNamespaces {
			if _, err := namespaces.EnsureExists(apiClient, serviceRegistration.Service.Namespace, r.CrossNSACLPolicy); err != nil {
				r.Log.Error(err, "failed to ensure Consul namespace exists", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace, "consul ns", serviceRegistration.Service.Namespace)
				return err
			}
		}

		// Register the service instance with Consul.
		r.Log.Info("registering gateway with Consul", "name", serviceRegistration.Service.Service,
			"id", serviceRegistration.ID)
		_, err = apiClient.Catalog().Register(serviceRegistration, nil)
		if err != nil {
			r.Log.Error(err, "failed to register gateway", "name", serviceRegistration.Service.Service)
			return err
		}
	}

	return nil
}

// serviceName computes the service name to register with Consul from the pod and endpoints object. In a single port
// service, it defaults to the endpoints name, but can be overridden by a pod annotation. In a multi port service, the
// endpoints name is always used since the pod annotation will have multiple service names listed (one per port).
// Changing the Consul service name via annotations is not supported for multi port services.
func serviceName(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	svcName := serviceEndpoints.Name
	// If the annotation has a comma, it is a multi port Pod. In that case we always use the name of the endpoint.
	if serviceNameFromAnnotation, ok := pod.Annotations[constants.AnnotationService]; ok && serviceNameFromAnnotation != "" && !strings.Contains(serviceNameFromAnnotation, ",") {
		svcName = serviceNameFromAnnotation
	}
	return svcName
}

func serviceID(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	return fmt.Sprintf("%s-%s", pod.Name, serviceName(pod, serviceEndpoints))
}

func proxyServiceName(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	svcName := serviceName(pod, serviceEndpoints)
	return fmt.Sprintf("%s-sidecar-proxy", svcName)
}

func proxyServiceID(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	proxySvcName := proxyServiceName(pod, serviceEndpoints)
	return fmt.Sprintf("%s-%s", pod.Name, proxySvcName)
}

func annotationProxyConfigMap(pod corev1.Pod) (map[string]any, error) {
	parsed := make(map[string]any)
	if config, ok := pod.Annotations[constants.AnnotationProxyConfigMap]; ok && config != "" {
		err := json.Unmarshal([]byte(config), &parsed)
		if err != nil {
			// Always return an empty map on error
			return make(map[string]any), fmt.Errorf("unable to parse `%v` annotation for pod `%v`: %w", constants.AnnotationProxyConfigMap, pod.Name, err)
		}
	}
	return parsed, nil
}

// createServiceRegistrations creates the service and proxy service instance registrations with the information from the
// Pod.
func (r *Controller) createServiceRegistrations(pod corev1.Pod, serviceEndpoints corev1.Endpoints, healthStatus string) (*api.CatalogRegistration, *api.CatalogRegistration, error) {
	// If a port is specified, then we determine the value of that port
	// and register that port for the host service.
	// The meshWebhook will always set the port annotation if one is not provided on the pod.
	var consulServicePort int
	if raw, ok := pod.Annotations[constants.AnnotationPort]; ok && raw != "" {
		if multiPort := strings.Split(raw, ","); len(multiPort) > 1 {
			// Figure out which index of the ports annotation to use by
			// finding the index of the service names annotation.
			raw = multiPort[getMultiPortIdx(pod, serviceEndpoints)]
		}
		if port, err := common.PortValue(pod, raw); port > 0 {
			if err != nil {
				return nil, nil, err
			}
			consulServicePort = int(port)
		}
	}

	var node corev1.Node
	// Ignore errors because we don't want failures to block running services.
	_ = r.Client.Get(context.Background(), types.NamespacedName{Name: pod.Spec.NodeName, Namespace: pod.Namespace}, &node)
	locality := parseLocality(node)

	// We only want that annotation to be present when explicitly overriding the consul svc name
	// Otherwise, the Consul service name should equal the Kubernetes Service name.
	// The service name in Consul defaults to the Endpoints object name, and is overridden by the pod
	// annotation consul.hashicorp.com/connect-service..
	svcName := serviceName(pod, serviceEndpoints)

	svcID := serviceID(pod, serviceEndpoints)

	meta := map[string]string{
		constants.MetaKeyPodName: pod.Name,
		metaKeyKubeServiceName:   serviceEndpoints.Name,
		constants.MetaKeyKubeNS:  serviceEndpoints.Namespace,
		metaKeyManagedBy:         constants.ManagedByValue,
		constants.MetaKeyPodUID:  string(pod.UID),
		metaKeySyntheticNode:     "true",
	}
	for k, v := range pod.Annotations {
		if strings.HasPrefix(k, constants.AnnotationMeta) && strings.TrimPrefix(k, constants.AnnotationMeta) != "" {
			if v == "$POD_NAME" {
				meta[strings.TrimPrefix(k, constants.AnnotationMeta)] = pod.Name
			} else {
				meta[strings.TrimPrefix(k, constants.AnnotationMeta)] = v
			}
		}
	}
	tags := consulTags(pod)

	consulNS := r.consulNamespace(pod.Namespace)

	service := &api.AgentService{
		ID:        svcID,
		Service:   svcName,
		Port:      consulServicePort,
		Address:   pod.Status.PodIP,
		Meta:      meta,
		Namespace: consulNS,
		Tags:      tags,
		Locality:  locality,
	}
	serviceRegistration := &api.CatalogRegistration{
		Node:    common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName),
		Address: pod.Status.HostIP,
		NodeMeta: map[string]string{
			metaKeySyntheticNode: "true",
		},
		Service: service,
		Check: &api.AgentCheck{
			CheckID:   consulHealthCheckID(pod.Namespace, svcID),
			Name:      constants.ConsulKubernetesCheckName,
			Type:      constants.ConsulKubernetesCheckType,
			Status:    healthStatus,
			ServiceID: svcID,
			Output:    getHealthCheckStatusReason(healthStatus, pod.Name, pod.Namespace),
			Namespace: consulNS,
		},
		SkipNodeUpdate: true,
	}
	r.appendNodeMeta(serviceRegistration)

	proxySvcName := proxyServiceName(pod, serviceEndpoints)
	proxySvcID := proxyServiceID(pod, serviceEndpoints)

	// Set the default values from the annotation, if possible.
	baseConfig, err := annotationProxyConfigMap(pod)
	if err != nil {
		r.Log.Error(err, "annotation unable to be applied")
	}
	proxyConfig := &api.AgentServiceConnectProxyConfig{
		DestinationServiceName: svcName,
		DestinationServiceID:   svcID,
		Config:                 baseConfig,
	}

	// If metrics are enabled, the proxyConfig should set envoy_prometheus_bind_addr to a listener on 0.0.0.0 on
	// the PrometheusScrapePort that points to a metrics backend. The backend for this listener will be determined by
	// the envoy bootstrapping command (consul connect envoy) configuration in the init container. If there is a merged
	// metrics server, the backend would be that server. If we are not running the merged metrics server, the backend
	// should just be the Envoy metrics endpoint.
	enableMetrics, err := r.MetricsConfig.EnableMetrics(pod)
	if err != nil {
		return nil, nil, err
	}
	if enableMetrics {
		prometheusScrapePort, err := r.MetricsConfig.PrometheusScrapePort(pod)
		if err != nil {
			return nil, nil, err
		}
		prometheusScrapeListener := fmt.Sprintf("0.0.0.0:%s", prometheusScrapePort)
		proxyConfig.Config[envoyPrometheusBindAddr] = prometheusScrapeListener
	}

	if r.EnableTelemetryCollector && proxyConfig.Config != nil {
		proxyConfig.Config[envoyTelemetryCollectorBindSocketDir] = "/consul/connect-inject"
	}

	if consulServicePort > 0 {
		proxyConfig.LocalServiceAddress = "127.0.0.1"
		proxyConfig.LocalServicePort = consulServicePort
	}

	upstreams, err := r.processUpstreams(pod, serviceEndpoints)
	if err != nil {
		return nil, nil, err
	}
	proxyConfig.Upstreams = upstreams

	proxyPort := constants.ProxyDefaultInboundPort
	if idx := getMultiPortIdx(pod, serviceEndpoints); idx >= 0 {
		proxyPort += idx
	}
	proxyService := &api.AgentService{
		Kind:      api.ServiceKindConnectProxy,
		ID:        proxySvcID,
		Service:   proxySvcName,
		Port:      proxyPort,
		Address:   pod.Status.PodIP,
		Meta:      meta,
		Namespace: consulNS,
		Proxy:     proxyConfig,
		Tags:      tags,
		// Sidecar locality (not proxied service locality) is used for locality-aware routing.
		Locality: locality,
	}

	// A user can enable/disable tproxy for an entire namespace.
	var ns corev1.Namespace
	err = r.Client.Get(r.Context, types.NamespacedName{Name: pod.Namespace, Namespace: ""}, &ns)
	if err != nil {
		return nil, nil, err
	}

	tproxyEnabled, err := common.TransparentProxyEnabled(ns, pod, r.EnableTransparentProxy)
	if err != nil {
		return nil, nil, err
	}

	if tproxyEnabled {
		var k8sService corev1.Service
		proxyService.Proxy.Mode = api.ProxyModeTransparent
		err = r.Client.Get(r.Context, types.NamespacedName{Name: serviceEndpoints.Name, Namespace: serviceEndpoints.Namespace}, &k8sService)
		if err != nil {
			return nil, nil, err
		}

		// Check if the service has a valid IP.
		parsedIP := net.ParseIP(k8sService.Spec.ClusterIP)
		if parsedIP != nil {
			taggedAddresses := make(map[string]api.ServiceAddress)

			// When a service has multiple ports, we need to choose the port that is registered with Consul
			// and only set that port as the tagged address because Consul currently does not support multiple ports
			// on a single service.
			var k8sServicePort int32
			for _, sp := range k8sService.Spec.Ports {
				targetPortValue, err := portValueFromIntOrString(pod, sp.TargetPort)
				if err != nil {
					return nil, nil, err
				}

				// If the targetPortValue is not zero and is the consulServicePort, then this is the service port we'll use as the tagged address.
				if targetPortValue != 0 && targetPortValue == consulServicePort {
					k8sServicePort = sp.Port
					break
				} else {
					// If targetPort is not specified, then the service port is used as the target port,
					// and we can compare the service port with the Consul service port.
					if sp.Port == int32(consulServicePort) {
						k8sServicePort = sp.Port
						break
					}
				}
			}

			taggedAddresses[clusterIPTaggedAddressName] = api.ServiceAddress{
				Address: k8sService.Spec.ClusterIP,
				Port:    int(k8sServicePort),
			}

			service.TaggedAddresses = taggedAddresses
			proxyService.TaggedAddresses = taggedAddresses

		} else {
			r.Log.Info("skipping syncing service cluster IP to Consul", "name", k8sService.Name, "ns", k8sService.Namespace, "ip", k8sService.Spec.ClusterIP)
		}

		// Expose k8s probes as Envoy listeners if needed.
		overwriteProbes, err := common.ShouldOverwriteProbes(pod, r.TProxyOverwriteProbes)
		if err != nil {
			return nil, nil, err
		}
		if overwriteProbes {
			var originalPod corev1.Pod
			err = json.Unmarshal([]byte(pod.Annotations[constants.AnnotationOriginalPod]), &originalPod)
			if err != nil {
				return nil, nil, err
			}

			for _, mutatedContainer := range pod.Spec.Containers {
				for _, originalContainer := range originalPod.Spec.Containers {
					if originalContainer.Name == mutatedContainer.Name {
						if mutatedContainer.LivenessProbe != nil && mutatedContainer.LivenessProbe.HTTPGet != nil {
							originalLivenessPort, err := portValueFromIntOrString(originalPod, originalContainer.LivenessProbe.HTTPGet.Port)
							if err != nil {
								return nil, nil, err
							}
							proxyConfig.Expose.Paths = append(proxyConfig.Expose.Paths, api.ExposePath{
								ListenerPort:  mutatedContainer.LivenessProbe.HTTPGet.Port.IntValue(),
								LocalPathPort: originalLivenessPort,
								Path:          mutatedContainer.LivenessProbe.HTTPGet.Path,
							})
						}
						if mutatedContainer.ReadinessProbe != nil && mutatedContainer.ReadinessProbe.HTTPGet != nil {
							originalReadinessPort, err := portValueFromIntOrString(originalPod, originalContainer.ReadinessProbe.HTTPGet.Port)
							if err != nil {
								return nil, nil, err
							}
							proxyConfig.Expose.Paths = append(proxyConfig.Expose.Paths, api.ExposePath{
								ListenerPort:  mutatedContainer.ReadinessProbe.HTTPGet.Port.IntValue(),
								LocalPathPort: originalReadinessPort,
								Path:          mutatedContainer.ReadinessProbe.HTTPGet.Path,
							})
						}
						if mutatedContainer.StartupProbe != nil && mutatedContainer.StartupProbe.HTTPGet != nil {
							originalStartupPort, err := portValueFromIntOrString(originalPod, originalContainer.StartupProbe.HTTPGet.Port)
							if err != nil {
								return nil, nil, err
							}
							proxyConfig.Expose.Paths = append(proxyConfig.Expose.Paths, api.ExposePath{
								ListenerPort:  mutatedContainer.StartupProbe.HTTPGet.Port.IntValue(),
								LocalPathPort: originalStartupPort,
								Path:          mutatedContainer.StartupProbe.HTTPGet.Path,
							})
						}
					}
				}
			}
		}
	}

	proxyServiceRegistration := &api.CatalogRegistration{
		Node:    common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName),
		Address: pod.Status.HostIP,
		NodeMeta: map[string]string{
			metaKeySyntheticNode: "true",
		},
		Service: proxyService,
		Check: &api.AgentCheck{
			CheckID:   consulHealthCheckID(pod.Namespace, proxySvcID),
			Name:      constants.ConsulKubernetesCheckName,
			Type:      constants.ConsulKubernetesCheckType,
			Status:    healthStatus,
			ServiceID: proxySvcID,
			Output:    getHealthCheckStatusReason(healthStatus, pod.Name, pod.Namespace),
			Namespace: consulNS,
		},
		SkipNodeUpdate: true,
	}
	r.appendNodeMeta(proxyServiceRegistration)

	return serviceRegistration, proxyServiceRegistration, nil
}

// createGatewayRegistrations creates the gateway service registrations with the information from the Pod.
func (r *Controller) createGatewayRegistrations(pod corev1.Pod, serviceEndpoints corev1.Endpoints, healthStatus string) (*api.CatalogRegistration, error) {
	meta := map[string]string{
		constants.MetaKeyPodName: pod.Name,
		metaKeyKubeServiceName:   serviceEndpoints.Name,
		constants.MetaKeyKubeNS:  serviceEndpoints.Namespace,
		metaKeyManagedBy:         constants.ManagedByValue,
		metaKeySyntheticNode:     "true",
		constants.MetaKeyPodUID:  string(pod.UID),
	}

	// Set the default values from the annotation, if possible.
	baseConfig, err := annotationProxyConfigMap(pod)
	if err != nil {
		r.Log.Error(err, "annotation unable to be applied")
	}

	service := &api.AgentService{
		ID:      pod.Name,
		Address: pod.Status.PodIP,
		Meta:    meta,
		Proxy: &api.AgentServiceConnectProxyConfig{
			Config: baseConfig,
		},
	}

	gatewayServiceName, ok := pod.Annotations[constants.AnnotationGatewayConsulServiceName]
	if !ok {
		return nil, fmt.Errorf("failed to read annontation %s from pod %s/%s", constants.AnnotationGatewayConsulServiceName, pod.Namespace, pod.Name)
	}
	service.Service = gatewayServiceName

	var consulNS string

	// Set the service values.
	switch pod.Annotations[constants.AnnotationGatewayKind] {
	case meshGateway:
		service.Kind = api.ServiceKindMeshGateway
		if r.EnableConsulNamespaces {
			service.Namespace = defaultNS
			consulNS = defaultNS
		}

		port, err := strconv.Atoi(pod.Annotations[constants.AnnotationMeshGatewayContainerPort])
		if err != nil {
			return nil, err
		}
		service.Port = port

		if r.EnableWANFederation {
			meta[metaKeyConsulWANFederation] = "1"
		}

		wanAddr, wanPort, err := r.getWanData(pod, serviceEndpoints)
		if err != nil {
			return nil, err
		}
		service.TaggedAddresses = map[string]api.ServiceAddress{
			"lan": {
				Address: pod.Status.PodIP,
				Port:    port,
			},
			"wan": {
				Address: wanAddr,
				Port:    wanPort,
			},
		}
	case terminatingGateway:
		service.Kind = api.ServiceKindTerminatingGateway
		service.Port = 8443
		if ns, ok := pod.Annotations[constants.AnnotationGatewayNamespace]; ok && r.EnableConsulNamespaces {
			service.Namespace = ns
			consulNS = ns
		}
	case ingressGateway:
		service.Kind = api.ServiceKindIngressGateway
		if ns, ok := pod.Annotations[constants.AnnotationGatewayNamespace]; ok && r.EnableConsulNamespaces {
			service.Namespace = ns
			consulNS = ns
		}

		wanAddr, wanPort, err := r.getWanData(pod, serviceEndpoints)
		if err != nil {
			return nil, err
		}
		service.Port = 21000
		service.TaggedAddresses = map[string]api.ServiceAddress{
			"lan": {
				Address: pod.Status.PodIP,
				Port:    21000,
			},
			"wan": {
				Address: wanAddr,
				Port:    wanPort,
			},
		}
		service.Proxy.Config["envoy_gateway_no_default_bind"] = true
		service.Proxy.Config["envoy_gateway_bind_addresses"] = map[string]interface{}{
			"all-interfaces": map[string]interface{}{
				"address": "0.0.0.0",
			},
		}
	case apiGateway:
		// Do nothing. This is only here so that API gateway pods have annotations
		// consistent with other gateway types but don't return an error below.
	default:
		return nil, fmt.Errorf("%s must be one of %s, %s, %s, or %s", constants.AnnotationGatewayKind, meshGateway, terminatingGateway, ingressGateway, apiGateway)
	}

	if r.MetricsConfig.DefaultEnableMetrics && r.MetricsConfig.EnableGatewayMetrics {
		service.Proxy.Config["envoy_prometheus_bind_addr"] = fmt.Sprintf("%s:20200", pod.Status.PodIP)
	}

	if r.EnableTelemetryCollector && service.Proxy != nil && service.Proxy.Config != nil {
		service.Proxy.Config[envoyTelemetryCollectorBindSocketDir] = "/consul/service"
	}

	serviceRegistration := &api.CatalogRegistration{
		Node:    common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName),
		Address: pod.Status.HostIP,
		NodeMeta: map[string]string{
			metaKeySyntheticNode: "true",
		},
		Service: service,
		Check: &api.AgentCheck{
			CheckID:   consulHealthCheckID(pod.Namespace, pod.Name),
			Name:      constants.ConsulKubernetesCheckName,
			Type:      constants.ConsulKubernetesCheckType,
			Status:    healthStatus,
			ServiceID: pod.Name,
			Namespace: consulNS,
			Output:    getHealthCheckStatusReason(healthStatus, pod.Name, pod.Namespace),
		},
		SkipNodeUpdate: true,
	}
	r.appendNodeMeta(serviceRegistration)

	return serviceRegistration, nil
}

func (r *Controller) getWanData(pod corev1.Pod, endpoints corev1.Endpoints) (string, int, error) {
	var wanAddr string
	source, ok := pod.Annotations[constants.AnnotationGatewayWANSource]
	if !ok {
		return "", 0, fmt.Errorf("failed to read annotation %s", constants.AnnotationGatewayWANSource)
	}
	switch source {
	case "NodeName":
		wanAddr = pod.Spec.NodeName
	case "NodeIP":
		wanAddr = pod.Status.HostIP
	case "Static":
		wanAddr = pod.Annotations[constants.AnnotationGatewayWANAddress]
	case "Service":
		svc, err := r.getService(endpoints)
		if err != nil {
			return "", 0, fmt.Errorf("failed to read service %s in namespace %s", endpoints.Name, endpoints.Namespace)
		}
		switch svc.Spec.Type {
		case corev1.ServiceTypeNodePort:
			wanAddr = pod.Status.HostIP
		case corev1.ServiceTypeClusterIP:
			wanAddr = svc.Spec.ClusterIP
		case corev1.ServiceTypeLoadBalancer:
			if len(svc.Status.LoadBalancer.Ingress) == 0 {
				return "", 0, fmt.Errorf("failed to read ingress config for loadbalancer for service %s in namespace %s", endpoints.Name, endpoints.Namespace)
			}
			for _, ingr := range svc.Status.LoadBalancer.Ingress {
				if ingr.IP != "" {
					wanAddr = ingr.IP
					break
				} else if ingr.Hostname != "" {
					wanAddr = ingr.Hostname
					break
				}
			}
		}
	}

	wanPort, err := strconv.Atoi(pod.Annotations[constants.AnnotationGatewayWANPort])
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse WAN port from value %s", pod.Annotations[constants.AnnotationGatewayWANPort])
	}
	return wanAddr, wanPort, nil
}

func (r *Controller) getService(endpoints corev1.Endpoints) (*corev1.Service, error) {
	var svc corev1.Service
	if err := r.Client.Get(r.Context, types.NamespacedName{Namespace: endpoints.Namespace, Name: endpoints.Name}, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

// portValueFromIntOrString returns the integer port value from the port that can be
// a named port, an integer string (e.g. "80"), or an integer. If the port is a named port,
// this function will attempt to find the value from the containers of the pod.
func portValueFromIntOrString(pod corev1.Pod, port intstr.IntOrString) (int, error) {
	if port.Type == intstr.Int {
		return port.IntValue(), nil
	}

	// Otherwise, find named port or try to parse the string as an int.
	portVal, err := common.PortValue(pod, port.StrVal)
	if err != nil {
		return 0, err
	}
	return int(portVal), nil
}

// consulHealthCheckID deterministically generates a health check ID based on service ID and Kubernetes namespace.
func consulHealthCheckID(k8sNS string, serviceID string) string {
	return fmt.Sprintf("%s/%s", k8sNS, serviceID)
}

// getHealthCheckStatusReason takes an Consul's health check status (either passing or critical)
// as well as pod name and namespace and returns the reason message.
func getHealthCheckStatusReason(healthCheckStatus, podName, podNamespace string) string {
	if healthCheckStatus == api.HealthPassing {
		return constants.KubernetesSuccessReasonMsg
	}

	return fmt.Sprintf("Pod \"%s/%s\" is not ready", podNamespace, podName)
}

// deregisterService queries all services on the node for service instances that have the metadata
// "k8s-service-name"=k8sSvcName and "k8s-namespace"=k8sSvcNamespace. The k8s service name may or may not match the
// consul service name, but the k8s service name will always match the metadata on the Consul service
// "k8s-service-name". So, we query Consul services by "k8s-service-name" metadata.
// When querying by the k8s service name and namespace, the request will return service instances and
// associated proxy service instances.
// The argument deregisterEndpointAddress decides whether to deregister *all* service instances or selectively deregister
// them only if they are not in deregisterEndpointAddress. If the map is nil, it will deregister all instances. If the map
// has addresses, it will only deregister instances not in the map.
// If the pod backing a Consul service instance still exists and the graceful shutdown lifecycle mode is enabled, the instance
// will not be deregistered. Instead, its health check will be updated to Critical in order to drain incoming traffic and
// this function will return a requeueAfter duration. This can be used to requeue the event at the longest shutdown time
// interval to clean up these instances after they have exited.
func (r *Controller) deregisterService(
	ctx context.Context,
	apiClient *api.Client,
	k8sSvcName string,
	k8sSvcNamespace string,
	deregisterEndpointAddress map[string]bool) (time.Duration, error) {

	// Get services matching metadata from Consul
	serviceInstances, err := r.serviceInstances(apiClient, k8sSvcName, k8sSvcNamespace)
	if err != nil {
		r.Log.Error(err, "failed to get service instances", "name", k8sSvcName)
		return 0, err
	}

	var errs error
	var requeueAfter time.Duration
	for _, svc := range serviceInstances {
		// We need to get services matching "k8s-service-name" and "k8s-namespace" metadata.
		// If we selectively deregister, only deregister if the address is not in the map. Otherwise, deregister
		// every service instance.
		var serviceDeregistered bool

		if deregister(svc.ServiceAddress, deregisterEndpointAddress) {
			// If graceful shutdown is enabled, continue to the next service instance and
			// mark that an event requeue is needed. We should requeue at the longest time interval
			// to prevent excessive re-queues. Also, updating the health status in Consul to Critical
			// should prevent routing during gracefulShutdown.
			podShutdownDuration, err := r.getGracefulShutdownAndUpdatePodCheck(ctx, apiClient, svc, k8sSvcNamespace)
			if err != nil {
				r.Log.Error(err, "failed to get pod shutdown duration", "svc", svc.ServiceName)
				errs = multierror.Append(errs, err)
			}

			// set requeue response, then continue to the next service instance
			if podShutdownDuration > requeueAfter {
				requeueAfter = podShutdownDuration
			}
			if podShutdownDuration > 0 {
				continue
			}

			// If the service address is not in the Endpoints addresses, deregister it.
			r.Log.Info("deregistering service from consul", "svc", svc.ServiceID)
			_, err = apiClient.Catalog().Deregister(&api.CatalogDeregistration{
				Node:      svc.Node,
				ServiceID: svc.ServiceID,
				Namespace: svc.Namespace,
			}, nil)
			if err != nil {
				// Do not exit right away as there might be other services that need to be deregistered.
				r.Log.Error(err, "failed to deregister service instance", "id", svc.ServiceID)
				errs = multierror.Append(errs, err)
			} else {
				serviceDeregistered = true
			}
		}

		if r.AuthMethod != "" && serviceDeregistered {
			r.Log.Info("reconciling ACL tokens for service", "svc", svc.ServiceName)
			err := r.deleteACLTokensForServiceInstance(apiClient, svc, k8sSvcNamespace, svc.ServiceMeta[constants.MetaKeyPodName], svc.ServiceMeta[constants.MetaKeyPodUID])
			if err != nil {
				r.Log.Error(err, "failed to reconcile ACL tokens for service", "svc", svc.ServiceName)
				errs = multierror.Append(errs, err)
			}
		}

		if serviceDeregistered {
			err = r.deregisterNode(apiClient, svc.Node)
			if err != nil {
				r.Log.Error(err, "failed to deregister node", "svc", svc.ServiceName)
				errs = multierror.Append(errs, err)
			}
		}
	}

	if requeueAfter > 0 {
		r.Log.Info("re-queueing event for graceful shutdown", "name", k8sSvcName, "k8sNamespace", k8sSvcNamespace, "requeueAfter", requeueAfter)
	}

	return requeueAfter, errs
}

// getGracefulShutdownAndUpdatePodCheck checks if the pod is in the process of being terminated and if so, updates the
// health status of the service to critical. It returns the duration for which the pod should be re-queued (which is the pods
// gracefulShutdownPeriod setting).
func (r *Controller) getGracefulShutdownAndUpdatePodCheck(ctx context.Context, apiClient *api.Client, svc *api.CatalogService, k8sNamespace string) (time.Duration, error) {
	// Get the pod, and check if it is still running. We do this to defer ACL/node cleanup for pods that are
	// in graceful termination
	podName := svc.ServiceMeta[constants.MetaKeyPodName]
	if podName == "" {
		return 0, nil
	}

	var pod corev1.Pod
	err := r.Client.Get(ctx, types.NamespacedName{Name: podName, Namespace: k8sNamespace}, &pod)
	if k8serrors.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		r.Log.Error(err, "failed to get terminating pod", "name", podName, "k8sNamespace", k8sNamespace)
		return 0, fmt.Errorf("failed to get terminating pod %s/%s: %w", k8sNamespace, podName, err)
	}

	// In a statefulset rollout, pods can go down and a new pod will come back up with the same name but a different uid
	// or node name. In older consul-k8s patches, the pod uid may not be set, so to account for that we also check if
	// the node changed, since newer service instances should exist for the new node. In that case, it's not the old pod
	// that is gracefully shutting down; the old pod is gone and we should deregister that old instance from Consul.
	if string(pod.UID) != svc.ServiceMeta[constants.MetaKeyPodUID] || common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName) != svc.Node {
		return 0, nil
	}

	shutdownSeconds, err := r.getGracefulShutdownPeriodSecondsForPod(pod)
	if err != nil {
		r.Log.Error(err, "failed to get graceful shutdown period for pod", "name", pod, "k8sNamespace", k8sNamespace)
		return 0, fmt.Errorf("failed to get graceful shutdown period for pod %s/%s: %w", k8sNamespace, podName, err)
	}

	if shutdownSeconds > 0 {
		// Update the health status of the service to critical so that we can drain inbound traffic.
		// We don't need to handle the proxy service since that will be reconciled looping through all the service instances.
		serviceRegistration := &api.CatalogRegistration{
			Node:    svc.Node,
			Address: pod.Status.HostIP,
			// Service is nil since we are patching the health status
			Check: &api.AgentCheck{
				CheckID:   consulHealthCheckID(pod.Namespace, svc.ServiceID),
				Name:      constants.ConsulKubernetesCheckName,
				Type:      constants.ConsulKubernetesCheckType,
				Status:    api.HealthCritical,
				ServiceID: svc.ServiceID,
				Output:    fmt.Sprintf("Pod \"%s/%s\" is terminating", pod.Namespace, podName),
				Namespace: r.consulNamespace(pod.Namespace),
			},
			SkipNodeUpdate: true,
		}

		r.Log.Info("updating health status of service with Consul to critical in order to drain inbound traffic", "name", svc.ServiceName,
			"id", svc.ServiceID, "pod", podName, "k8sNamespace", pod.Namespace)
		_, err = apiClient.Catalog().Register(serviceRegistration, nil)
		if err != nil {
			r.Log.Error(err, "failed to update service health status to critical", "name", svc.ServiceName, "pod", podName)
			return 0, fmt.Errorf("failed to update service health status for pod %s/%s to critical: %w", pod.Namespace, podName, err)
		}

		// Return the duration for which the pod should be re-queued. We add 20% to the shutdownSeconds to account for
		// any potential delay in the pod killed.
		return time.Duration(shutdownSeconds+int(math.Ceil(float64(shutdownSeconds)*0.2))) * time.Second, nil
	}
	return 0, nil
}

// getGracefulShutdownPeriodSecondsForPod returns the graceful shutdown period for the pod. If one is not specified,
// either through the controller configuration or pod annotations, it returns 0.
func (r *Controller) getGracefulShutdownPeriodSecondsForPod(pod corev1.Pod) (int, error) {
	enabled, err := r.LifecycleConfig.EnableProxyLifecycle(pod)
	if err != nil {
		return 0, fmt.Errorf("failed to get parse proxy lifecycle configuration for pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	// Check that SidecarProxyLifecycle is enabled.
	if !enabled {
		return 0, nil
	}

	return r.LifecycleConfig.ShutdownGracePeriodSeconds(pod)
}

// deregisterNode removes a node if it does not have any associated services attached to it.
// When using Consul Enterprise, serviceInstancesForK8SServiceNameAndNamespace will search across all namespaces
// (wildcard) to determine if there any other services associated with the Node. We also only search for nodes that have
// the correct kubernetes metadata (managed-by-endpoints-controller and synthetic-node).
func (r *Controller) deregisterNode(apiClient *api.Client, nodeName string) error {
	var (
		serviceList *api.CatalogNodeServiceList
		err         error
	)

	filter := fmt.Sprintf(`Meta[%q] == %q and Meta[%q] == %q`,
		"synthetic-node", "true", metaKeyManagedBy, constants.ManagedByValue)
	if r.EnableConsulNamespaces {
		serviceList, _, err = apiClient.Catalog().NodeServiceList(nodeName, &api.QueryOptions{Filter: filter, Namespace: namespaces.WildcardNamespace})
	} else {
		serviceList, _, err = apiClient.Catalog().NodeServiceList(nodeName, &api.QueryOptions{Filter: filter})
	}
	if err != nil {
		return fmt.Errorf("failed to get a list of node services: %s", err)
	}

	if len(serviceList.Services) == 0 {
		r.Log.Info("deregistering node from consul", "node", nodeName, "services", serviceList.Services)
		_, err := apiClient.Catalog().Deregister(&api.CatalogDeregistration{Node: nodeName}, nil)
		if err != nil {
			r.Log.Error(err, "failed to deregister node", "name", nodeName)
		}
	}
	return nil
}

// deleteACLTokensForServiceInstance finds the ACL tokens that belongs to the service instance and deletes it from Consul.
// It will only check for ACL tokens that have been created with the auth method this controller
// has been configured with and will only delete tokens for the provided podName and podUID.
func (r *Controller) deleteACLTokensForServiceInstance(apiClient *api.Client, svc *api.CatalogService, k8sNS, podName, podUID string) error {
	// Skip if podName is empty.
	if podName == "" {
		return nil
	}

	// Note that while the `TokenListFiltered` query below should only return a subset
	// of tokens from the Consul servers, it will return an unfiltered list on older
	// versions of Consul (because they do not yet support the query parameter).
	// To be safe, we still need to iterate over tokens and assert the service name
	// matches as well.
	tokens, _, err := apiClient.ACL().TokenListFiltered(
		api.ACLTokenFilterOptions{
			ServiceName: svc.ServiceName,
		},
		&api.QueryOptions{
			Namespace: svc.Namespace,
		})
	if err != nil {
		return fmt.Errorf("failed to get a list of tokens from Consul: %s", err)
	}

	for _, token := range tokens {
		// Only delete tokens that:
		// * have been created with the auth method configured for this endpoints controller
		// * have a single service identity whose service name is the same as 'svc.Service'
		if token.AuthMethod == r.AuthMethod &&
			len(token.ServiceIdentities) == 1 &&
			token.ServiceIdentities[0].ServiceName == svc.ServiceName {
			tokenMeta, err := getTokenMetaFromDescription(token.Description)
			if err != nil {
				return fmt.Errorf("failed to parse token metadata: %s", err)
			}

			tokenPodName := strings.TrimPrefix(tokenMeta[tokenMetaPodNameKey], k8sNS+"/")

			// backward compability logic on token with no podUID in metadata
			podUIDMatched := tokenMeta[constants.MetaKeyPodUID] == podUID || tokenMeta[constants.MetaKeyPodUID] == ""

			// If we can't find token's pod, delete it.
			if tokenPodName == podName && podUIDMatched {
				r.Log.Info("deleting ACL token for pod", "name", podName)
				if _, err := apiClient.ACL().TokenDelete(token.AccessorID, &api.WriteOptions{Namespace: svc.Namespace}); err != nil {
					return fmt.Errorf("failed to delete token from Consul: %s", err)
				}
			}
		}
	}
	return nil
}

// processUpstreams reads the list of upstreams from the Pod annotation and converts them into a list of api.Upstream
// objects.
func (r *Controller) processUpstreams(pod corev1.Pod, endpoints corev1.Endpoints) ([]api.Upstream, error) {
	// In a multiport pod, only the first service's proxy should have upstreams configured. This skips configuring
	// upstreams on additional services on the pod.
	mpIdx := getMultiPortIdx(pod, endpoints)
	if mpIdx > 0 {
		return []api.Upstream{}, nil
	}

	var upstreams []api.Upstream
	if raw, ok := pod.Annotations[constants.AnnotationUpstreams]; ok && raw != "" {
		for _, raw := range strings.Split(raw, ",") {
			var upstream api.Upstream

			// parts separates out the port, and determines whether it's a prepared query or not, since parts[0] would
			// be "prepared_query" if it is.
			parts := strings.SplitN(raw, ":", 3)

			// serviceParts helps determine which format of upstream we're processing,
			// [service-name].[service-namespace].[service-partition]:[port]:[optional datacenter]
			// or
			// [service-name].svc.[service-namespace].ns.[service-peer].peer:[port]
			// [service-name].svc.[service-namespace].ns.[service-partition].ap:[port]
			// [service-name].svc.[service-namespace].ns.[service-datacenter].dc:[port]
			labeledFormat := false
			serviceParts := strings.Split(parts[0], ".")
			if len(serviceParts) >= 2 {
				if serviceParts[1] == "svc" {
					labeledFormat = true
				}
			}

			if strings.TrimSpace(parts[0]) == "prepared_query" {
				upstream = processPreparedQueryUpstream(pod, raw)
			} else if labeledFormat {
				var err error
				upstream, err = r.processLabeledUpstream(pod, raw)
				if err != nil {
					return []api.Upstream{}, err
				}
			} else {
				var err error
				upstream, err = r.processUnlabeledUpstream(pod, raw)
				if err != nil {
					return []api.Upstream{}, err
				}
			}

			upstreams = append(upstreams, upstream)
		}
	}

	return upstreams, nil
}

// getTokenMetaFromDescription parses JSON metadata from token's description.
func getTokenMetaFromDescription(description string) (map[string]string, error) {
	re := regexp.MustCompile(`.*({.+})`)

	matches := re.FindStringSubmatch(description)
	if len(matches) != 2 {
		return nil, fmt.Errorf("failed to extract token metadata from description: %s", description)
	}
	tokenMetaJSON := matches[1]

	var tokenMeta map[string]string
	err := json.Unmarshal([]byte(tokenMetaJSON), &tokenMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal token metadata '%s': %s", tokenMetaJSON, err)
	}

	return tokenMeta, nil
}

func (r *Controller) serviceInstances(apiClient *api.Client, k8sServiceName, k8sServiceNamespace string) ([]*api.CatalogService, error) {
	var (
		instances []*api.CatalogService
		errs      error
	)

	// Get the names of services that have the provided k8sServiceName and k8sServiceNamespace in their metadata.
	// This is necessary so that we can then list the service instances for each Consul service name, which may
	// not match the K8s service name.
	services, err := r.servicesForK8SServiceNameAndNamespace(apiClient, k8sServiceName, k8sServiceNamespace)
	if err != nil {
		r.Log.Error(err, "failed to get catalog services", "name", k8sServiceName)
		return nil, err
	}

	// Query consul catalog for a list of service instances matching the given service names.
	filter := fmt.Sprintf(`NodeMeta[%q] == %q and ServiceMeta[%q] == %q and ServiceMeta[%q] == %q and ServiceMeta[%q] == %q`,
		metaKeySyntheticNode, "true",
		metaKeyKubeServiceName, k8sServiceName,
		constants.MetaKeyKubeNS, k8sServiceNamespace,
		metaKeyManagedBy, constants.ManagedByValue)
	for _, service := range services {
		var is []*api.CatalogService
		// Always query the default NS. This ensures that we include mesh gateways, which are always registered to the default NS.
		// It also ensures that during migrations that enable namespaces, we deregister old service instances in the default NS.
		// An alternative approach to this dual query would be a service instance list using the wildcard NS, which would also
		// include instances in Consul namespaces that are no longer in use (e.g. configuration change); this capability does
		// not currently exist in Consul's catalog API (as of 1.17) and would need to first be added.
		//
		// This request uses the service index of the services table (does not perform a full table scan), then decorates each
		// result with a single node fetched by ID index from the nodes table.
		is, _, err = apiClient.Catalog().Service(service, "", &api.QueryOptions{Filter: filter})
		if err != nil {
			errs = multierror.Append(errs, err)
		} else {
			instances = append(instances, is...)
		}
		// If namespaces are enabled a non-default NS is targeted, also query by target Consul NS.
		if r.EnableConsulNamespaces {
			nonDefaultNamespace := namespaces.NonDefaultConsulNamespace(r.consulNamespace(k8sServiceNamespace))
			if nonDefaultNamespace != "" {
				is, _, err = apiClient.Catalog().Service(service, "", &api.QueryOptions{Filter: filter, Namespace: nonDefaultNamespace})
				if err != nil {
					errs = multierror.Append(errs, err)
				} else {
					instances = append(instances, is...)
				}
			}
		}
	}

	return instances, errs
}

// servicesForK8SServiceNameAndNamespace calls Consul's Services to get the list
// of services that have the provided k8sServiceName and k8sServiceNamespace in their metadata.
func (r *Controller) servicesForK8SServiceNameAndNamespace(apiClient *api.Client, k8sServiceName, k8sServiceNamespace string) ([]string, error) {
	var (
		services map[string][]string
		err      error
	)
	filter := fmt.Sprintf(`ServiceMeta[%q] == %q and ServiceMeta[%q] == %q and ServiceMeta[%q] == %q`,
		metaKeyKubeServiceName, k8sServiceName, constants.MetaKeyKubeNS, k8sServiceNamespace, metaKeyManagedBy, constants.ManagedByValue)
	// Always query the default NS. This ensures that we cover CE->Ent upgrades where services were previously
	// in the default NS, as well as mesh gateways, which are always registered to the default NS.
	//
	// This request performs a NS-bound scan of the services table. If needed in the future, its performance
	// could be improved by adding an index on ServiceMeta to Consul's state store.
	services, _, err = apiClient.Catalog().Services(&api.QueryOptions{Filter: filter})
	if err != nil {
		return nil, err
	}
	// If namespaces are enabled a non-default NS is targeted, also query by target Consul NS.
	if r.EnableConsulNamespaces {
		nonDefaultNamespace := namespaces.NonDefaultConsulNamespace(r.consulNamespace(k8sServiceNamespace))
		if nonDefaultNamespace != "" {
			ss, _, err := apiClient.Catalog().Services(&api.QueryOptions{Filter: filter, Namespace: nonDefaultNamespace})
			if err != nil {
				return nil, err
			}
			// Add to existing map to deduplicate.
			for s := range ss {
				services[s] = nil // We don't use the tags, so just set to nil
			}
		}
	}

	// Return just the service name keys (we don't need the tags)
	// https://developer.hashicorp.com/consul/api-docs/catalog#list-services
	var serviceNames []string
	for s := range services {
		serviceNames = append(serviceNames, s)
	}
	return serviceNames, err
}

// processPreparedQueryUpstream processes an upstream in the format:
// prepared_query:[query name]:[port].
func processPreparedQueryUpstream(pod corev1.Pod, rawUpstream string) api.Upstream {
	var preparedQuery string
	var port int32
	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = common.PortValue(pod, strings.TrimSpace(parts[2]))
	preparedQuery = strings.TrimSpace(parts[1])
	var upstream api.Upstream
	if port > 0 {
		upstream = api.Upstream{
			DestinationType: api.UpstreamDestTypePreparedQuery,
			DestinationName: preparedQuery,
			LocalBindPort:   int(port),
		}
	}
	return upstream
}

// processUnlabeledUpstream processes an upstream in the format:
// [service-name].[service-namespace].[service-partition]:[port]:[optional datacenter].
// There is no unlabeled field for peering.
func (r *Controller) processUnlabeledUpstream(pod corev1.Pod, rawUpstream string) (api.Upstream, error) {
	var datacenter, svcName, namespace, partition string
	var port int32
	var upstream api.Upstream

	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = common.PortValue(pod, strings.TrimSpace(parts[1]))

	// If Consul Namespaces or Admin Partitions are enabled, attempt to parse the
	// upstream for a namespace.
	if r.EnableConsulNamespaces || r.EnableConsulPartitions {
		pieces := strings.SplitN(parts[0], ".", 3)
		switch len(pieces) {
		case 3:
			partition = strings.TrimSpace(pieces[2])
			fallthrough
		case 2:
			namespace = strings.TrimSpace(pieces[1])
			fallthrough
		default:
			svcName = strings.TrimSpace(pieces[0])
		}
	} else {
		svcName = strings.TrimSpace(parts[0])
	}

	// parse the optional datacenter
	if len(parts) > 2 {
		datacenter = strings.TrimSpace(parts[2])
	}
	if port > 0 {
		upstream = api.Upstream{
			DestinationType:      api.UpstreamDestTypeService,
			DestinationPartition: partition,
			DestinationPeer:      "",
			DestinationNamespace: namespace,
			DestinationName:      svcName,
			Datacenter:           datacenter,
			LocalBindPort:        int(port),
		}
	}
	return upstream, nil
}

// processLabeledUpstream processes an upstream in the format:
// [service-name].svc.[service-namespace].ns.[service-peer].peer:[port]
// [service-name].svc.[service-namespace].ns.[service-partition].ap:[port]
// [service-name].svc.[service-namespace].ns.[service-datacenter].dc:[port].
func (r *Controller) processLabeledUpstream(pod corev1.Pod, rawUpstream string) (api.Upstream, error) {
	var datacenter, svcName, namespace, partition, peer string
	var port int32
	var upstream api.Upstream

	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = common.PortValue(pod, strings.TrimSpace(parts[1]))

	service := parts[0]

	pieces := strings.Split(service, ".")

	if r.EnableConsulNamespaces || r.EnableConsulPartitions {
		switch len(pieces) {
		case 6:
			end := strings.TrimSpace(pieces[5])
			switch end {
			case "peer":
				peer = strings.TrimSpace(pieces[4])
			case "ap":
				partition = strings.TrimSpace(pieces[4])
			case "dc":
				datacenter = strings.TrimSpace(pieces[4])
			default:
				return api.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			fallthrough
		case 4:
			if strings.TrimSpace(pieces[3]) == "ns" {
				namespace = strings.TrimSpace(pieces[2])
			} else {
				return api.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			fallthrough
		case 2:
			if strings.TrimSpace(pieces[1]) == "svc" {
				svcName = strings.TrimSpace(pieces[0])
			}
		default:
			return api.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
		}
	} else {
		switch len(pieces) {
		case 4:
			end := strings.TrimSpace(pieces[3])
			switch end {
			case "peer":
				peer = strings.TrimSpace(pieces[2])
			case "dc":
				datacenter = strings.TrimSpace(pieces[2])
			default:
				return api.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
			}
			fallthrough
		case 2:
			svcName = strings.TrimSpace(pieces[0])
		default:
			return api.Upstream{}, fmt.Errorf("upstream structured incorrectly: %s", rawUpstream)
		}
	}

	if port > 0 {
		upstream = api.Upstream{
			DestinationType:      api.UpstreamDestTypeService,
			DestinationPartition: partition,
			DestinationPeer:      peer,
			DestinationNamespace: namespace,
			DestinationName:      svcName,
			Datacenter:           datacenter,
			LocalBindPort:        int(port),
		}
	}
	return upstream, nil
}

// consulNamespace returns the Consul destination namespace for a provided Kubernetes namespace
// depending on Consul Namespaces being enabled and the value of namespace mirroring.
func (r *Controller) consulNamespace(namespace string) string {
	return namespaces.ConsulNamespace(namespace, r.EnableConsulNamespaces, r.ConsulDestinationNamespace, r.EnableNSMirroring, r.NSMirroringPrefix)
}

func (r *Controller) appendNodeMeta(registration *api.CatalogRegistration) {
	for k, v := range r.NodeMeta {
		registration.NodeMeta[k] = v
	}
}

// assignServiceVirtualIPs manually assigns the ClusterIP to the virtual IP table so that transparent proxy routing works.
func assignServiceVirtualIP(ctx context.Context, apiClient *api.Client, svc *api.AgentService) error {
	ip := svc.TaggedAddresses[clusterIPTaggedAddressName].Address
	if ip == "" {
		return nil
	}

	_, _, err := apiClient.Internal().AssignServiceVirtualIP(ctx, svc.Service, []string{ip}, &api.WriteOptions{Namespace: svc.Namespace, Partition: svc.Partition})
	if err != nil {
		// Maintain backwards compatibility with older versions of Consul that do not support the VIP improvements. Tproxy
		// will not work 100% correctly but the mesh will still work
		if strings.Contains(err.Error(), "404") {
			return fmt.Errorf("failed to add ip for service %s to virtual ip table. Please upgrade Consul to version 1.16 or higher", svc.Service)
		} else {
			return err
		}
	}
	return nil
}

// hasBeenInjected checks the value of the status annotation and returns true if the Pod has been injected.
func hasBeenInjected(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[constants.KeyInjectStatus]; ok && anno == constants.Injected {
		return true
	}
	return false
}

// isGateway checks the value of the gateway annotation and returns true if the Pod
// represents a Gateway kind that should be acted upon by the endpoints controller.
func isGateway(pod corev1.Pod) bool {
	anno, ok := pod.Annotations[constants.AnnotationGatewayKind]
	return ok && anno != "" && anno != apiGateway
}

// isTelemetryCollector checks whether a pod is part of a deployment for a Consul Telemetry Collector. If so,
// and this is the first pod deployed to a Namespace, we need to create the Namespace in Consul. Otherwise the
// deployment may fail out during service registration because it is deployed to a Namespace that does not exist.
func isTelemetryCollector(pod corev1.Pod) bool {
	anno, ok := pod.Annotations[constants.LabelTelemetryCollector]
	return ok && anno != ""
}

// ensureNamespaceExists creates a Consul namespace for a pod in the event it does not exist.
// At the time of writing, we use this for the Consul Telemetry Collector which may be the first
// pod deployed to a namespace. If it is, it's connect-inject will fail for lack of a namespace.
func (r *Controller) ensureNamespaceExists(apiClient *api.Client, pod corev1.Pod) error {
	if r.EnableConsulNamespaces {
		consulNS := r.consulNamespace(pod.Namespace)
		if _, err := namespaces.EnsureExists(apiClient, consulNS, r.CrossNSACLPolicy); err != nil {
			r.Log.Error(err, "failed to ensure Consul namespace exists", "ns", pod.Namespace, "consul ns", consulNS)
			return err
		}
	}
	return nil
}

// mapAddresses combines all addresses to a mapping of address to its health status.
func mapAddresses(addresses corev1.EndpointSubset) map[corev1.EndpointAddress]string {
	m := make(map[corev1.EndpointAddress]string)
	for _, readyAddress := range addresses.Addresses {
		m[readyAddress] = api.HealthPassing
	}

	for _, notReadyAddress := range addresses.NotReadyAddresses {
		m[notReadyAddress] = api.HealthCritical
	}

	return m
}

// isLabeledIgnore checks the value of the label `consul.hashicorp.com/service-ignore` and returns true if the
// label exists and is "truthy". Otherwise, it returns false.
func isLabeledIgnore(labels map[string]string) bool {
	value, labelExists := labels[constants.LabelServiceIgnore]
	shouldIgnore, err := strconv.ParseBool(value)

	return shouldIgnore && labelExists && err == nil
}

// consulTags returns tags that should be added to the Consul service and proxy registrations.
func consulTags(pod corev1.Pod) []string {
	var tags []string
	if raw, ok := pod.Annotations[constants.AnnotationTags]; ok && raw != "" {
		tags = append(tags, parsetags.ParseTags(raw)...)
	}

	var interpolatedTags []string
	for _, t := range tags {
		// Support light interpolation to preserve backwards compatibility where tags could
		// be environment variables.
		// Right now the only string we interpolate is $POD_NAME since that's all
		// users have asked for as of now. More can be added here in the future.
		if t == "$POD_NAME" {
			t = pod.Name
		}
		interpolatedTags = append(interpolatedTags, t)
	}

	return interpolatedTags
}

func getMultiPortIdx(pod corev1.Pod, serviceEndpoints corev1.Endpoints) int {
	for i, name := range strings.Split(pod.Annotations[constants.AnnotationService], ",") {
		if name == serviceName(pod, serviceEndpoints) {
			return i
		}
	}
	return -1
}

// deregister returns that the address is marked for deregistration if the map is nil or if the address is explicitly
// marked in the map for deregistration.
func deregister(address string, deregisterEndpointAddress map[string]bool) bool {
	if deregisterEndpointAddress == nil {
		return true
	}
	deregister, ok := deregisterEndpointAddress[address]
	if ok {
		return deregister
	}
	return true
}
