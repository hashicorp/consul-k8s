package connectinject

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	MetaKeyPodName             = "pod-name"
	MetaKeyKubeServiceName     = "k8s-service-name"
	MetaKeyKubeNS              = "k8s-namespace"
	MetaKeyManagedBy           = "managed-by"
	TokenMetaPodNameKey        = "pod"
	kubernetesSuccessReasonMsg = "Kubernetes health checks passing"
	envoyPrometheusBindAddr    = "envoy_prometheus_bind_addr"
	sidecarContainer           = "consul-dataplane"

	// clusterIPTaggedAddressName is the key for the tagged address to store the service's cluster IP and service port
	// in Consul. Note: This value should not be changed without a corresponding change in Consul.
	clusterIPTaggedAddressName = "virtual"

	// exposedPathsLivenessPortsRangeStart is the start of the port range that we will use as
	// the ListenerPort for the Expose configuration of the proxy registration for a liveness probe.
	exposedPathsLivenessPortsRangeStart = 20300

	// exposedPathsReadinessPortsRangeStart is the start of the port range that we will use as
	// the ListenerPort for the Expose configuration of the proxy registration for a readiness probe.
	exposedPathsReadinessPortsRangeStart = 20400

	// exposedPathsStartupPortsRangeStart is the start of the port range that we will use as
	// the ListenerPort for the Expose configuration of the proxy registration for a startup probe.
	exposedPathsStartupPortsRangeStart = 20500

	// ConsulNodeName is the node name that we'll use to register and deregister services.
	ConsulNodeName = "k8s-service-mesh"

	// ConsulNodeAddress is the address of the consul node (defined by ConsulNodeName).
	// This address does not need to be routable as this node is ephemeral, and we're only providing it because
	// Consul's API currently requires node address to be provided when registering a node.
	ConsulNodeAddress = "127.0.0.1"

	// ConsulKubernetesCheckType is the type of health check in Consul for Kubernetes readiness status.
	ConsulKubernetesCheckType = "kubernetes-readiness"

	// ConsulKubernetesCheckName is the name of health check in Consul for Kubernetes readiness status.
	ConsulKubernetesCheckName = "Kubernetes Readiness Check"

	// EnvoyInboundListenerPort is the port where envoy's inbound listener is listening.
	EnvoyInboundListenerPort = 20000
)

type EndpointsController struct {
	client.Client
	// ConsulClient is the client to use for API calls to Consul.
	ConsulClient *api.Client
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
	// ConsulAPITimeout is the duration that the consul API client will
	// wait for a response from the API before cancelling the request.
	ConsulAPITimeout time.Duration

	MetricsConfig MetricsConfig
	Log           logr.Logger

	Scheme *runtime.Scheme
	context.Context
}

// Reconcile reads the state of an Endpoints object for a Kubernetes Service and reconciles Consul services which
// correspond to the Kubernetes Service. These events are driven by changes to the Pods backing the Kube service.
func (r *EndpointsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var errs error
	var serviceEndpoints corev1.Endpoints

	// Ignore the request if the namespace of the endpoint is not allowed.
	if shouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	err := r.Client.Get(ctx, req.NamespacedName, &serviceEndpoints)

	// endpointPods holds a set of all pods this endpoints object is currently pointing to.
	// We use this later when we reconcile ACL tokens to decide whether an ACL token in Consul
	// is for a pod that no longer exists.
	endpointPods := mapset.NewSet()

	// If the endpoints object has been deleted (and we get an IsNotFound
	// error), we need to deregister all instances in Consul for that service.
	if k8serrors.IsNotFound(err) {
		// Deregister all instances in Consul for this service. The function deregisterService handles
		// the case where the Consul service name is different from the Kubernetes service name.
		err = r.deregisterService(req.Name, req.Namespace, nil)
		return ctrl.Result{}, err
	} else if err != nil {
		r.Log.Error(err, "failed to get Endpoints", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)

	// If the endpoints object has the label "consul.hashicorp.com/service-ignore" set to true, deregister all instances in Consul for this service.
	// It is possible that the endpoints object has never been registered, in which case deregistration is a no-op.
	if isLabeledIgnore(serviceEndpoints.Labels) {
		// We always deregister the service to handle the case where a user has registered the service, then added the label later.
		r.Log.Info("Ignoring endpoint labeled with `consul.hashicorp.com/service-ignore: \"true\"`", "name", req.Name, "namespace", req.Namespace)
		err = r.deregisterService(req.Name, req.Namespace, nil)
		return ctrl.Result{}, err
	}

	// endpointAddressMap stores every IP that corresponds to a Pod in the Endpoints object. It is used to compare
	// against service instances in Consul to deregister them if they are not in the map.
	endpointAddressMap := map[string]bool{}

	// Register all addresses of this Endpoints object as service instances in Consul.
	for _, subset := range serviceEndpoints.Subsets {
		for address, healthStatus := range mapAddresses(subset) {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				var pod corev1.Pod
				objectKey := types.NamespacedName{Name: address.TargetRef.Name, Namespace: address.TargetRef.Namespace}
				if err = r.Client.Get(ctx, objectKey, &pod); err != nil {
					r.Log.Error(err, "failed to get pod", "name", address.TargetRef.Name)
					errs = multierror.Append(errs, err)
					continue
				}

				svcName, ok := pod.Annotations[annotationKubernetesService]
				if ok && serviceEndpoints.Name != svcName {
					r.Log.Info("ignoring endpoint because it doesn't match explicit service annotation", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
					// deregistration for service instances that don't match the annotation happens later because we don't add this pod to the endpointAddressMap.
					continue
				}

				if hasBeenInjected(pod) {
					endpointPods.Add(address.TargetRef.Name)
					if err = r.registerServicesAndHealthCheck(pod, serviceEndpoints, healthStatus, endpointAddressMap); err != nil {
						r.Log.Error(err, "failed to register services or health check", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
						errs = multierror.Append(errs, err)
					}
				}
			}
		}
	}

	// Compare service instances in Consul with addresses in Endpoints. If an address is not in Endpoints, deregister
	// from Consul. This uses endpointAddressMap which is populated with the addresses in the Endpoints object during
	// the registration codepath.
	if err = r.deregisterService(serviceEndpoints.Name, serviceEndpoints.Namespace, endpointAddressMap); err != nil {
		r.Log.Error(err, "failed to deregister endpoints", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
		errs = multierror.Append(errs, err)
	}

	return ctrl.Result{}, errs
}

func (r *EndpointsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *EndpointsController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(r)
}

// registerServicesAndHealthCheck creates Consul registrations for the service and proxy and registers them with Consul.
// It also upserts a Kubernetes health check for the service based on whether the endpoint address is ready.
func (r *EndpointsController) registerServicesAndHealthCheck(pod corev1.Pod, serviceEndpoints corev1.Endpoints, healthStatus string, endpointAddressMap map[string]bool) error {
	// Build the endpointAddressMap up for deregistering service instances later.
	endpointAddressMap[pod.Status.PodIP] = true

	var managedByEndpointsController bool
	if raw, ok := pod.Labels[keyManagedBy]; ok && raw == managedByValue {
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
			"id", serviceRegistration.ID)
		_, err = r.ConsulClient.Catalog().Register(serviceRegistration, nil)
		if err != nil {
			r.Log.Error(err, "failed to register service", "name", serviceRegistration.Service.Service)
			return err
		}

		// Register the proxy service instance with Consul.
		r.Log.Info("registering proxy service with Consul", "name", proxyServiceRegistration.Service.Service)
		_, err = r.ConsulClient.Catalog().Register(proxyServiceRegistration, nil)
		if err != nil {
			r.Log.Error(err, "failed to register proxy service", "name", proxyServiceRegistration.Service.Service)
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
	if serviceNameFromAnnotation, ok := pod.Annotations[annotationService]; ok && serviceNameFromAnnotation != "" && !strings.Contains(serviceNameFromAnnotation, ",") {
		svcName = serviceNameFromAnnotation
	}
	return svcName
}

func serviceID(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	return fmt.Sprintf("%s-%s", pod.Name, serviceName(pod, serviceEndpoints))
}

func proxyServiceName(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	serviceName := serviceName(pod, serviceEndpoints)
	return fmt.Sprintf("%s-sidecar-proxy", serviceName)
}

func proxyServiceID(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	proxyServiceName := proxyServiceName(pod, serviceEndpoints)
	return fmt.Sprintf("%s-%s", pod.Name, proxyServiceName)
}

// createServiceRegistrations creates the service and proxy service instance registrations with the information from the
// Pod.
func (r *EndpointsController) createServiceRegistrations(pod corev1.Pod, serviceEndpoints corev1.Endpoints, healthStatus string) (*api.CatalogRegistration, *api.CatalogRegistration, error) {
	// If a port is specified, then we determine the value of that port
	// and register that port for the host service.
	// The meshWebhook will always set the port annotation if one is not provided on the pod.
	var consulServicePort int
	if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
		if multiPort := strings.Split(raw, ","); len(multiPort) > 1 {
			// Figure out which index of the ports annotation to use by
			// finding the index of the service names annotation.
			raw = multiPort[getMultiPortIdx(pod, serviceEndpoints)]
		}
		if port, err := portValue(pod, raw); port > 0 {
			if err != nil {
				return nil, nil, err
			}
			consulServicePort = int(port)
		}
	}

	// We only want that annotation to be present when explicitly overriding the consul svc name
	// Otherwise, the Consul service name should equal the Kubernetes Service name.
	// The service name in Consul defaults to the Endpoints object name, and is overridden by the pod
	// annotation consul.hashicorp.com/connect-service..
	svcName := serviceName(pod, serviceEndpoints)

	svcID := serviceID(pod, serviceEndpoints)

	meta := map[string]string{
		MetaKeyPodName:         pod.Name,
		MetaKeyKubeServiceName: serviceEndpoints.Name,
		MetaKeyKubeNS:          serviceEndpoints.Namespace,
		MetaKeyManagedBy:       managedByValue,
	}
	for k, v := range pod.Annotations {
		if strings.HasPrefix(k, annotationMeta) && strings.TrimPrefix(k, annotationMeta) != "" {
			if v == "$POD_NAME" {
				meta[strings.TrimPrefix(k, annotationMeta)] = pod.Name
			} else {
				meta[strings.TrimPrefix(k, annotationMeta)] = v
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
	}
	serviceRegistration := &api.CatalogRegistration{
		Node:    ConsulNodeName,
		Address: ConsulNodeAddress,
		Service: service,
		Check: &api.AgentCheck{
			CheckID:   consulHealthCheckID(pod.Namespace, svcID),
			Name:      ConsulKubernetesCheckName,
			Type:      ConsulKubernetesCheckType,
			Status:    healthStatus,
			ServiceID: svcID,
			Output:    getHealthCheckStatusReason(healthStatus, pod.Name, pod.Namespace),
			Namespace: consulNS,
		},
		SkipNodeUpdate: true,
	}

	proxySvcName := proxyServiceName(pod, serviceEndpoints)
	proxySvcID := proxyServiceID(pod, serviceEndpoints)
	proxyConfig := &api.AgentServiceConnectProxyConfig{
		DestinationServiceName: svcName,
		DestinationServiceID:   svcID,
		Config:                 make(map[string]interface{}),
	}

	// If metrics are enabled, the proxyConfig should set envoy_prometheus_bind_addr to a listener on 0.0.0.0 on
	// the prometheusScrapePort that points to a metrics backend. The backend for this listener will be determined by
	// the envoy bootstrapping command (consul connect envoy) configuration in the init container. If there is a merged
	// metrics server, the backend would be that server. If we are not running the merged metrics server, the backend
	// should just be the Envoy metrics endpoint.
	enableMetrics, err := r.MetricsConfig.enableMetrics(pod)
	if err != nil {
		return nil, nil, err
	}
	if enableMetrics {
		prometheusScrapePort, err := r.MetricsConfig.prometheusScrapePort(pod)
		if err != nil {
			return nil, nil, err
		}
		prometheusScrapeListener := fmt.Sprintf("0.0.0.0:%s", prometheusScrapePort)
		proxyConfig.Config[envoyPrometheusBindAddr] = prometheusScrapeListener
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

	proxyPort := EnvoyInboundListenerPort
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
	}

	// A user can enable/disable tproxy for an entire namespace.
	var ns corev1.Namespace
	err = r.Client.Get(r.Context, types.NamespacedName{Name: pod.Namespace, Namespace: ""}, &ns)
	if err != nil {
		return nil, nil, err
	}

	tproxyEnabled, err := transparentProxyEnabled(ns, pod, r.EnableTransparentProxy)
	if err != nil {
		return nil, nil, err
	}

	if tproxyEnabled {
		var k8sService corev1.Service

		err := r.Client.Get(r.Context, types.NamespacedName{Name: serviceEndpoints.Name, Namespace: serviceEndpoints.Namespace}, &k8sService)
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

			proxyService.Proxy.Mode = api.ProxyModeTransparent
		} else {
			r.Log.Info("skipping syncing service cluster IP to Consul", "name", k8sService.Name, "ns", k8sService.Namespace, "ip", k8sService.Spec.ClusterIP)
		}

		// Expose k8s probes as Envoy listeners if needed.
		overwriteProbes, err := shouldOverwriteProbes(pod, r.TProxyOverwriteProbes)
		if err != nil {
			return nil, nil, err
		}
		if overwriteProbes {
			var originalPod corev1.Pod
			err := json.Unmarshal([]byte(pod.Annotations[annotationOriginalPod]), &originalPod)
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
		Node:    ConsulNodeName,
		Address: ConsulNodeAddress,
		Service: proxyService,
		Check: &api.AgentCheck{
			CheckID:   consulHealthCheckID(pod.Namespace, proxySvcID),
			Name:      ConsulKubernetesCheckName,
			Type:      ConsulKubernetesCheckType,
			Status:    healthStatus,
			ServiceID: proxySvcID,
			Output:    getHealthCheckStatusReason(healthStatus, pod.Name, pod.Namespace),
			Namespace: consulNS,
		},
		SkipNodeUpdate: true,
	}

	return serviceRegistration, proxyServiceRegistration, nil
}

// portValueFromIntOrString returns the integer port value from the port that can be
// a named port, an integer string (e.g. "80"), or an integer. If the port is a named port,
// this function will attempt to find the value from the containers of the pod.
func portValueFromIntOrString(pod corev1.Pod, port intstr.IntOrString) (int, error) {
	if port.Type == intstr.Int {
		return port.IntValue(), nil
	}

	// Otherwise, find named port or try to parse the string as an int.
	portVal, err := portValue(pod, port.StrVal)
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
		return kubernetesSuccessReasonMsg
	}

	return fmt.Sprintf("Pod \"%s/%s\" is not ready", podNamespace, podName)
}

// deregisterService queries all services on the node for service instances that have the metadata
// "k8s-service-name"=k8sSvcName and "k8s-namespace"=k8sSvcNamespace. The k8s service name may or may not match the
// consul service name, but the k8s service name will always match the metadata on the Consul service
// "k8s-service-name". So, we query Consul services by "k8s-service-name" metadata.
// When querying by the k8s service name and namespace, the request will return service instances and
// associated proxy service instances.
// The argument endpointsAddressesMap decides whether to deregister *all* service instances or selectively deregister
// them only if they are not in endpointsAddressesMap. If the map is nil, it will deregister all instances. If the map
// has addresses, it will only deregister instances not in the map.
func (r *EndpointsController) deregisterService(k8sSvcName, k8sSvcNamespace string, endpointsAddressesMap map[string]bool) error {
	// We need to get services matching "k8s-service-name" and "k8s-namespace" metadata.
	consulNamespace := r.consulNamespace(k8sSvcNamespace)

	// Get services matching metadata.
	svcs, err := r.serviceInstancesForK8SServiceNameAndNamespace(k8sSvcName, k8sSvcNamespace)
	if err != nil {
		r.Log.Error(err, "failed to get service instances", "name", k8sSvcName)
		return err
	}

	// Deregister each service instance that matches the metadata.
	for _, svc := range svcs.Services {
		// If we selectively deregister, only deregister if the address is not in the map. Otherwise, deregister
		// every service instance.
		var serviceDeregistered bool
		if endpointsAddressesMap != nil {
			if _, ok := endpointsAddressesMap[svc.Address]; !ok {
				// If the service address is not in the Endpoints addresses, deregister it.
				r.Log.Info("deregistering service from consul", "svc", svc.ID)
				_, err = r.ConsulClient.Catalog().Deregister(&api.CatalogDeregistration{
					Node:      ConsulNodeName,
					ServiceID: svc.ID,
					Namespace: consulNamespace,
				}, nil)
				if err != nil {
					r.Log.Error(err, "failed to deregister service instance", "id", svc.ID)
					return err
				}
				serviceDeregistered = true
			}
		} else {
			r.Log.Info("deregistering service from consul", "svc", svc.ID)
			if _, err = r.ConsulClient.Catalog().Deregister(&api.CatalogDeregistration{
				Node:      ConsulNodeName,
				ServiceID: svc.ID,
				Namespace: consulNamespace,
			}, nil); err != nil {
				r.Log.Error(err, "failed to deregister service instance", "id", svc.ID)
				return err
			}
			serviceDeregistered = true
		}

		if r.AuthMethod != "" && serviceDeregistered {
			r.Log.Info("reconciling ACL tokens for service", "svc", svc.Service)
			err = r.deleteACLTokensForServiceInstance(svc.Service, k8sSvcNamespace, svc.Meta[MetaKeyPodName])
			if err != nil {
				r.Log.Error(err, "failed to reconcile ACL tokens for service", "svc", svc.Service)
				return err
			}
		}
	}

	return nil
}

// deleteACLTokensForServiceInstance finds the ACL tokens that belongs to the service instance and deletes it from Consul.
// It will only check for ACL tokens that have been created with the auth method this controller
// has been configured with and will only delete tokens for the provided podName.
func (r *EndpointsController) deleteACLTokensForServiceInstance(serviceName, k8sNS, podName string) error {
	// Skip if podName is empty.
	if podName == "" {
		return nil
	}

	consulNS := r.consulNamespace(k8sNS)
	tokens, _, err := r.ConsulClient.ACL().TokenList(&api.QueryOptions{
		Namespace: consulNS,
	})
	if err != nil {
		return fmt.Errorf("failed to get a list of tokens from Consul: %s", err)
	}

	for _, token := range tokens {
		// Only delete tokens that:
		// * have been created with the auth method configured for this endpoints controller
		// * have a single service identity whose service name is the same as 'serviceName'
		if token.AuthMethod == r.AuthMethod &&
			len(token.ServiceIdentities) == 1 &&
			token.ServiceIdentities[0].ServiceName == serviceName {
			tokenMeta, err := getTokenMetaFromDescription(token.Description)
			if err != nil {
				return fmt.Errorf("failed to parse token metadata: %s", err)
			}

			tokenPodName := strings.TrimPrefix(tokenMeta[TokenMetaPodNameKey], k8sNS+"/")

			// If we can't find token's pod, delete it.
			if tokenPodName == podName {
				r.Log.Info("deleting ACL token for pod", "name", podName)
				_, err = r.ConsulClient.ACL().TokenDelete(token.AccessorID, &api.WriteOptions{Namespace: consulNS})
				if err != nil {
					return fmt.Errorf("failed to delete token from Consul: %s", err)
				}
			} else if err != nil {
				return err
			}
		}
	}

	return nil
}

// processUpstreams reads the list of upstreams from the Pod annotation and converts them into a list of api.Upstream
// objects.
func (r *EndpointsController) processUpstreams(pod corev1.Pod, endpoints corev1.Endpoints) ([]api.Upstream, error) {
	// In a multiport pod, only the first service's proxy should have upstreams configured. This skips configuring
	// upstreams on additional services on the pod.
	mpIdx := getMultiPortIdx(pod, endpoints)
	if mpIdx > 0 {
		return []api.Upstream{}, nil
	}

	var upstreams []api.Upstream
	if raw, ok := pod.Annotations[annotationUpstreams]; ok && raw != "" {
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

// serviceInstancesForK8SServiceNameAndNamespace calls Consul's ServicesWithFilter to get the list
// of services instances that have the provided k8sServiceName and k8sServiceNamespace in their metadata.
func (r *EndpointsController) serviceInstancesForK8SServiceNameAndNamespace(k8sServiceName, k8sServiceNamespace string) (*api.CatalogNodeServiceList, error) {
	filter := fmt.Sprintf(`Meta[%q] == %q and Meta[%q] == %q and Meta[%q] == %q`,
		MetaKeyKubeServiceName, k8sServiceName, MetaKeyKubeNS, k8sServiceNamespace, MetaKeyManagedBy, managedByValue)

	serviceList, _, err := r.ConsulClient.Catalog().NodeServiceList(ConsulNodeName, &api.QueryOptions{Filter: filter, Namespace: r.consulNamespace(k8sServiceNamespace)})
	return serviceList, err
}

// processPreparedQueryUpstream processes an upstream in the format:
// prepared_query:[query name]:[port].
func processPreparedQueryUpstream(pod corev1.Pod, rawUpstream string) api.Upstream {
	var preparedQuery string
	var port int32
	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = portValue(pod, strings.TrimSpace(parts[2]))
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
func (r *EndpointsController) processUnlabeledUpstream(pod corev1.Pod, rawUpstream string) (api.Upstream, error) {
	var datacenter, serviceName, namespace, partition, peer string
	var port int32
	var upstream api.Upstream

	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = portValue(pod, strings.TrimSpace(parts[1]))

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
			serviceName = strings.TrimSpace(pieces[0])
		}
	} else {
		serviceName = strings.TrimSpace(parts[0])
	}

	// parse the optional datacenter
	if len(parts) > 2 {
		datacenter = strings.TrimSpace(parts[2])

		// Check if there's a proxy defaults config with mesh gateway
		// mode set to local or remote. This helps users from
		// accidentally forgetting to set a mesh gateway mode
		// and then being confused as to why their traffic isn't
		// routing.
		entry, _, err := r.ConsulClient.ConfigEntries().Get(api.ProxyDefaults, api.ProxyConfigGlobal, nil)
		if err != nil && strings.Contains(err.Error(), "Unexpected response code: 404") {
			return api.Upstream{}, fmt.Errorf("upstream %q is invalid: there is no ProxyDefaults config to set mesh gateway mode", rawUpstream)
		} else if err == nil {
			mode := entry.(*api.ProxyConfigEntry).MeshGateway.Mode
			if mode != api.MeshGatewayModeLocal && mode != api.MeshGatewayModeRemote {
				return api.Upstream{}, fmt.Errorf("upstream %q is invalid: ProxyDefaults mesh gateway mode is neither %q nor %q", rawUpstream, api.MeshGatewayModeLocal, api.MeshGatewayModeRemote)
			}
		}
		// NOTE: If we can't reach Consul we don't error out because
		// that would fail the pod scheduling and this is a nice-to-have
		// check, not something that should block during a Consul hiccup.
	}
	if port > 0 {
		upstream = api.Upstream{
			DestinationType:      api.UpstreamDestTypeService,
			DestinationPartition: partition,
			DestinationPeer:      peer,
			DestinationNamespace: namespace,
			DestinationName:      serviceName,
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
func (r *EndpointsController) processLabeledUpstream(pod corev1.Pod, rawUpstream string) (api.Upstream, error) {
	var datacenter, serviceName, namespace, partition, peer string
	var port int32
	var upstream api.Upstream

	parts := strings.SplitN(rawUpstream, ":", 3)

	port, _ = portValue(pod, strings.TrimSpace(parts[1]))

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
				serviceName = strings.TrimSpace(pieces[0])
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
			serviceName = strings.TrimSpace(pieces[0])
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
			DestinationName:      serviceName,
			Datacenter:           datacenter,
			LocalBindPort:        int(port),
		}
	}
	return upstream, nil

}

// shouldIgnore ignores namespaces where we don't connect-inject.
func shouldIgnore(namespace string, denySet, allowSet mapset.Set) bool {
	// Ignores system namespaces.
	if namespace == metav1.NamespaceSystem || namespace == metav1.NamespacePublic || namespace == "local-path-storage" {
		return true
	}

	// Ignores deny list.
	if denySet.Contains(namespace) {
		return true
	}

	// Ignores if not in allow list or allow list is not *.
	if !allowSet.Contains("*") && !allowSet.Contains(namespace) {
		return true
	}

	return false
}

// consulNamespace returns the Consul destination namespace for a provided Kubernetes namespace
// depending on Consul Namespaces being enabled and the value of namespace mirroring.
func (r *EndpointsController) consulNamespace(namespace string) string {
	return namespaces.ConsulNamespace(namespace, r.EnableConsulNamespaces, r.ConsulDestinationNamespace, r.EnableNSMirroring, r.NSMirroringPrefix)
}

// hasBeenInjected checks the value of the status annotation and returns true if the Pod has been injected.
func hasBeenInjected(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[keyInjectStatus]; ok && anno == injected {
		return true
	}
	return false
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
	value, labelExists := labels[labelServiceIgnore]
	shouldIgnore, err := strconv.ParseBool(value)

	return shouldIgnore && labelExists && err == nil
}

// consulTags returns tags that should be added to the Consul service and proxy registrations.
func consulTags(pod corev1.Pod) []string {
	var tags []string
	if raw, ok := pod.Annotations[annotationTags]; ok && raw != "" {
		tags = strings.Split(raw, ",")
	}
	// Get the tags from the deprecated tags annotation and combine.
	if raw, ok := pod.Annotations[annotationConnectTags]; ok && raw != "" {
		tags = append(tags, strings.Split(raw, ",")...)
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
	for i, name := range strings.Split(pod.Annotations[annotationService], ",") {
		if name == serviceName(pod, serviceEndpoints) {
			return i
		}
	}
	return -1
}
