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
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	MetaKeyPodName             = "pod-name"
	MetaKeyKubeServiceName     = "k8s-service-name"
	MetaKeyKubeNS              = "k8s-namespace"
	MetaKeyManagedBy           = "managed-by"
	TokenMetaPodNameKey        = "pod"
	kubernetesSuccessReasonMsg = "Kubernetes health checks passing"
	envoyPrometheusBindAddr    = "envoy_prometheus_bind_addr"
	envoySidecarContainer      = "envoy-sidecar"

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
)

type EndpointsController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	// ConsulClientCfg is the client config used by the ConsulClient when calling NewClient().
	ConsulClientCfg *api.Config
	// ConsulScheme is the scheme to use when making API calls to Consul,
	// i.e. "http" or "https".
	ConsulScheme string
	// ConsulPort is the port to make HTTP API calls to Consul agents on.
	ConsulPort string
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
		// Deregister all instances in Consul for this service. The function deregisterServiceOnAllAgents handles
		// the case where the Consul service name is different from the Kubernetes service name.
		err = r.deregisterServiceOnAllAgents(ctx, req.Name, req.Namespace, nil)
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
		err = r.deregisterServiceOnAllAgents(ctx, req.Name, req.Namespace, nil)
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
				if err := r.Client.Get(ctx, objectKey, &pod); err != nil {
					r.Log.Error(err, "failed to get pod", "name", address.TargetRef.Name)
					errs = multierror.Append(errs, err)
					continue
				}

				serviceName, ok := pod.Annotations[annotationKubernetesService]
				if ok && serviceEndpoints.Name != serviceName {
					r.Log.Info("ignoring endpoint because it doesn't match explicit service annotation", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
					// deregistration for service instances that don't match the annotation happens later because we don't add this pod to the endpointAddressMap.
					continue
				}

				if hasBeenInjected(pod) {
					endpointPods.Add(address.TargetRef.Name)
					if err := r.registerServicesAndHealthCheck(pod, serviceEndpoints, healthStatus, endpointAddressMap); err != nil {
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
	if err = r.deregisterServiceOnAllAgents(ctx, serviceEndpoints.Name, serviceEndpoints.Namespace, endpointAddressMap); err != nil {
		r.Log.Error(err, "failed to deregister endpoints on all agents", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
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
		Watches(
			&source.Kind{Type: &corev1.Pod{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForRunningAgentPods),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.filterAgentPods)),
		).Complete(r)
}

// registerServicesAndHealthCheck creates Consul registrations for the service and proxy and registers them with Consul.
// It also upserts a Kubernetes health check for the service based on whether the endpoint address is ready.
func (r *EndpointsController) registerServicesAndHealthCheck(pod corev1.Pod, serviceEndpoints corev1.Endpoints, healthStatus string, endpointAddressMap map[string]bool) error {
	podHostIP := pod.Status.HostIP

	if hasBeenInjected(pod) {
		// Build the endpointAddressMap up for deregistering service instances later.
		endpointAddressMap[pod.Status.PodIP] = true
		// Create client for Consul agent local to the pod.
		client, err := r.remoteConsulClient(podHostIP, r.consulNamespace(pod.Namespace))
		if err != nil {
			r.Log.Error(err, "failed to create a new Consul client", "address", podHostIP)
			return err
		}

		var managedByEndpointsController bool
		if raw, ok := pod.Labels[keyManagedBy]; ok && raw == managedByValue {
			managedByEndpointsController = true
		}
		// For pods managed by this controller, create and register the service instance.
		if managedByEndpointsController {
			// Get information from the pod to create service instance registrations.
			serviceRegistration, proxyServiceRegistration, err := r.createServiceRegistrations(pod, serviceEndpoints)
			if err != nil {
				r.Log.Error(err, "failed to create service registrations for endpoints", "name", serviceEndpoints.Name, "ns", serviceEndpoints.Namespace)
				return err
			}

			// Register the service instance with the local agent.
			// Note: the order of how we register services is important,
			// and the connect-proxy service should come after the "main" service
			// because its alias health check depends on the main service existing.
			r.Log.Info("registering service with Consul", "name", serviceRegistration.Name,
				"id", serviceRegistration.ID, "agentIP", podHostIP)
			err = client.Agent().ServiceRegister(serviceRegistration)
			if err != nil {
				r.Log.Error(err, "failed to register service", "name", serviceRegistration.Name)
				return err
			}

			// Register the proxy service instance with the local agent.
			r.Log.Info("registering proxy service with Consul", "name", proxyServiceRegistration.Name)
			err = client.Agent().ServiceRegister(proxyServiceRegistration)
			if err != nil {
				r.Log.Error(err, "failed to register proxy service", "name", proxyServiceRegistration.Name)
				return err
			}
		}

		// Update the service TTL health check for both legacy services and services managed by endpoints
		// controller. The proxy health checks are registered separately by endpoints controller and
		// lifecycle sidecar for legacy services. Here, we always update the health check for legacy and
		// newer services idempotently since the service health check is not added as part of the service
		// registration.
		reason := getHealthCheckStatusReason(healthStatus, pod.Name, pod.Namespace)
		serviceName := getServiceName(pod, serviceEndpoints)
		r.Log.Info("updating health check status for service", "name", serviceName, "reason", reason, "status", healthStatus)
		serviceID := getServiceID(pod, serviceEndpoints)
		healthCheckID := getConsulHealthCheckID(pod, serviceID)
		err = r.upsertHealthCheck(pod, client, serviceID, healthCheckID, healthStatus)
		if err != nil {
			r.Log.Error(err, "failed to update health check status for service", "name", serviceName)
			return err
		}
	}

	return nil
}

// getServiceCheck will return the health check for this pod and service if it exists.
func getServiceCheck(client *api.Client, healthCheckID string) (*api.AgentCheck, error) {
	filter := fmt.Sprintf("CheckID == `%s`", healthCheckID)
	checks, err := client.Agent().ChecksWithFilter(filter)
	if err != nil {
		return nil, err
	}
	// This will be nil (does not exist) or an actual check.
	return checks[healthCheckID], nil
}

// registerConsulHealthCheck registers a TTL health check for the service on this Agent local to the Pod. This will add
// the Pod's readiness status, which will mark the service instance healthy/unhealthy for Consul service mesh
// traffic.
func registerConsulHealthCheck(client *api.Client, consulHealthCheckID, serviceID, status string) error {
	// Create a TTL health check in Consul associated with this service and pod.
	// The TTL time is 100000h which should ensure that the check never fails due to timeout
	// of the TTL check.
	err := client.Agent().CheckRegister(&api.AgentCheckRegistration{
		ID:        consulHealthCheckID,
		Name:      "Kubernetes Health Check",
		ServiceID: serviceID,
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:                    "100000h",
			Status:                 status,
			SuccessBeforePassing:   1,
			FailuresBeforeCritical: 1,
		},
	})
	if err != nil {
		// Full error looks like:
		// Unexpected response code: 500 (ServiceID "consulnamespace/svc-id" does not exist)
		if strings.Contains(err.Error(), fmt.Sprintf("%s\" does not exist", serviceID)) {
			return fmt.Errorf("service %q not found in Consul: unable to register health check", serviceID)
		}
		return fmt.Errorf("registering health check for service %q: %w", serviceID, err)
	}

	return nil
}

// updateConsulHealthCheckStatus updates the consul health check status.
func (r *EndpointsController) updateConsulHealthCheckStatus(client *api.Client, consulHealthCheckID, status, reason string) error {
	r.Log.Info("updating health check", "id", consulHealthCheckID)
	err := client.Agent().UpdateTTL(consulHealthCheckID, reason, status)
	if err != nil {
		return fmt.Errorf("error updating health check: %w", err)
	}
	return nil
}

// upsertHealthCheck checks if the healthcheck exists for the service, and creates it if it doesn't exist, or updates it
// if it does.
func (r *EndpointsController) upsertHealthCheck(pod corev1.Pod, client *api.Client, serviceID, healthCheckID, status string) error {
	reason := getHealthCheckStatusReason(status, pod.Name, pod.Namespace)
	// Retrieve the health check that would exist if the service had one registered for this pod.
	serviceCheck, err := getServiceCheck(client, healthCheckID)
	if err != nil {
		return fmt.Errorf("unable to get agent health checks: serviceID=%s, checkID=%s, %s", serviceID, healthCheckID, err)
	}
	if serviceCheck == nil {
		// Create a new health check.
		err = registerConsulHealthCheck(client, healthCheckID, serviceID, status)
		if err != nil {
			return err
		}

		// Also update it, the reason this is separate is there is no way to set the Output field of the health check
		// at creation time, and this is what is displayed on the UI as opposed to the Notes field.
		err = r.updateConsulHealthCheckStatus(client, healthCheckID, status, reason)
		if err != nil {
			return err
		}
	} else if serviceCheck.Status != status {
		err = r.updateConsulHealthCheckStatus(client, healthCheckID, status, reason)
		if err != nil {
			return err
		}
	}
	return nil
}

// getServiceName computes the service name to register with Consul from the pod and endpoints object. In a single port
// service, it defaults to the endpoints name, but can be overridden by a pod annotation. In a multi port service, the
// endpoints name is always used since the pod annotation will have multiple service names listed (one per port).
// Changing the Consul service name via annotations is not supported for multi port services.
func getServiceName(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	serviceName := serviceEndpoints.Name
	// If the annotation has a comma, it is a multi port Pod. In that case we always use the name of the endpoint.
	if serviceNameFromAnnotation, ok := pod.Annotations[annotationService]; ok && serviceNameFromAnnotation != "" && !strings.Contains(serviceNameFromAnnotation, ",") {
		serviceName = serviceNameFromAnnotation
	}
	return serviceName
}

func getServiceID(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	return fmt.Sprintf("%s-%s", pod.Name, getServiceName(pod, serviceEndpoints))
}

func getProxyServiceName(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	serviceName := getServiceName(pod, serviceEndpoints)
	return fmt.Sprintf("%s-sidecar-proxy", serviceName)
}

func getProxyServiceID(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	proxyServiceName := getProxyServiceName(pod, serviceEndpoints)
	return fmt.Sprintf("%s-%s", pod.Name, proxyServiceName)
}

// createServiceRegistrations creates the service and proxy service instance registrations with the information from the
// Pod.
func (r *EndpointsController) createServiceRegistrations(pod corev1.Pod, serviceEndpoints corev1.Endpoints) (*api.AgentServiceRegistration, *api.AgentServiceRegistration, error) {
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
	serviceName := getServiceName(pod, serviceEndpoints)

	serviceID := getServiceID(pod, serviceEndpoints)

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

	service := &api.AgentServiceRegistration{
		ID:        serviceID,
		Name:      serviceName,
		Port:      consulServicePort,
		Address:   pod.Status.PodIP,
		Meta:      meta,
		Namespace: r.consulNamespace(pod.Namespace),
		Tags:      tags,
	}

	proxyServiceName := getProxyServiceName(pod, serviceEndpoints)
	proxyServiceID := getProxyServiceID(pod, serviceEndpoints)
	proxyConfig := &api.AgentServiceConnectProxyConfig{
		DestinationServiceName: serviceName,
		DestinationServiceID:   serviceID,
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

	proxyPort := 20000
	if idx := getMultiPortIdx(pod, serviceEndpoints); idx >= 0 {
		proxyPort += idx
	}
	proxyService := &api.AgentServiceRegistration{
		Kind:      api.ServiceKindConnectProxy,
		ID:        proxyServiceID,
		Name:      proxyServiceName,
		Port:      proxyPort,
		Address:   pod.Status.PodIP,
		Meta:      meta,
		Namespace: r.consulNamespace(pod.Namespace),
		Proxy:     proxyConfig,
		Checks: api.AgentServiceChecks{
			{
				Name:                           "Proxy Public Listener",
				TCP:                            fmt.Sprintf("%s:%d", pod.Status.PodIP, proxyPort),
				Interval:                       "10s",
				DeregisterCriticalServiceAfter: "10m",
			},
			{
				Name:         "Destination Alias",
				AliasService: serviceID,
			},
		},
		Tags: tags,
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

	return service, proxyService, nil
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

// getConsulHealthCheckID deterministically generates a health check ID that will be unique to the Agent
// where the health check is registered and deregistered.
func getConsulHealthCheckID(pod corev1.Pod, serviceID string) string {
	return fmt.Sprintf("%s/%s/kubernetes-health-check", pod.Namespace, serviceID)
}

// getHealthCheckStatusReason takes an Consul's health check status (either passing or critical)
// as well as pod name and namespace and returns the reason message.
func getHealthCheckStatusReason(healthCheckStatus, podName, podNamespace string) string {
	if healthCheckStatus == api.HealthPassing {
		return kubernetesSuccessReasonMsg
	}

	return fmt.Sprintf("Pod \"%s/%s\" is not ready", podNamespace, podName)
}

// deregisterServiceOnAllAgents queries all agents for service instances that have the metadata
// "k8s-service-name"=k8sSvcName and "k8s-namespace"=k8sSvcNamespace. The k8s service name may or may not match the
// consul service name, but the k8s service name will always match the metadata on the Consul service
// "k8s-service-name". So, we query Consul services by "k8s-service-name" metadata, which is only exposed on the agent
// API. Therefore, we need to query all agents who have services matching that metadata, and deregister each service
// instance. When querying by the k8s service name and namespace, the request will return service instances and
// associated proxy service instances.
// The argument endpointsAddressesMap decides whether to deregister *all* service instances or selectively deregister
// them only if they are not in endpointsAddressesMap. If the map is nil, it will deregister all instances. If the map
// has addresses, it will only deregister instances not in the map.
func (r *EndpointsController) deregisterServiceOnAllAgents(ctx context.Context, k8sSvcName, k8sSvcNamespace string, endpointsAddressesMap map[string]bool) error {
	// Get all agents by getting pods with label component=client, app=consul and release=<ReleaseName>
	agents := corev1.PodList{}
	listOptions := client.ListOptions{
		Namespace: r.ReleaseNamespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"component": "client",
			"app":       "consul",
			"release":   r.ReleaseName,
		}),
	}
	if err := r.Client.List(ctx, &agents, &listOptions); err != nil {
		r.Log.Error(err, "failed to get Consul client agent pods")
		return err
	}

	// On each agent, we need to get services matching "k8s-service-name" and "k8s-namespace" metadata.
	for _, agent := range agents.Items {
		ready := false
		for _, status := range agent.Status.Conditions {
			if status.Type == corev1.PodReady {
				ready = status.Status == corev1.ConditionTrue
			}
		}
		if !ready {
			// We can ignore this client agent here because once it switches its status from not-ready to ready,
			// we will reconcile all services as part of that event.
			r.Log.Info("Consul client agent is not ready, skipping deregistration", "consul-agent", agent.Name, "svc", k8sSvcName)
			continue
		}
		client, err := r.remoteConsulClient(agent.Status.PodIP, r.consulNamespace(k8sSvcNamespace))
		if err != nil {
			r.Log.Error(err, "failed to create a new Consul client", "address", agent.Status.PodIP)
			return err
		}

		// Get services matching metadata.
		svcs, err := serviceInstancesForK8SServiceNameAndNamespace(k8sSvcName, k8sSvcNamespace, client)
		if err != nil {
			r.Log.Error(err, "failed to get service instances", "name", k8sSvcName)
			return err
		}

		// Deregister each service instance that matches the metadata.
		for svcID, serviceRegistration := range svcs {
			// If we selectively deregister, only deregister if the address is not in the map. Otherwise, deregister
			// every service instance.
			var serviceDeregistered bool
			if endpointsAddressesMap != nil {
				if _, ok := endpointsAddressesMap[serviceRegistration.Address]; !ok {
					// If the service address is not in the Endpoints addresses, deregister it.
					r.Log.Info("deregistering service from consul", "svc", svcID)
					if err = client.Agent().ServiceDeregister(svcID); err != nil {
						r.Log.Error(err, "failed to deregister service instance", "id", svcID)
						return err
					}
					serviceDeregistered = true
				}
			} else {
				r.Log.Info("deregistering service from consul", "svc", svcID)
				if err = client.Agent().ServiceDeregister(svcID); err != nil {
					r.Log.Error(err, "failed to deregister service instance", "id", svcID)
					return err
				}
				serviceDeregistered = true
			}

			if r.AuthMethod != "" && serviceDeregistered {
				r.Log.Info("reconciling ACL tokens for service", "svc", serviceRegistration.Service)
				err = r.deleteACLTokensForServiceInstance(client, serviceRegistration.Service, k8sSvcNamespace, serviceRegistration.Meta[MetaKeyPodName])
				if err != nil {
					r.Log.Error(err, "failed to reconcile ACL tokens for service", "svc", serviceRegistration.Service)
					return err
				}
			}
		}
	}

	return nil
}

// deleteACLTokensForServiceInstance finds the ACL tokens that belongs to the service instance and deletes it from Consul.
// It will only check for ACL tokens that have been created with the auth method this controller
// has been configured with and will only delete tokens for the provided podName.
func (r *EndpointsController) deleteACLTokensForServiceInstance(client *api.Client, serviceName, k8sNS, podName string) error {
	// Skip if podName is empty.
	if podName == "" {
		return nil
	}

	tokens, _, err := client.ACL().TokenList(nil)
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
				_, err = client.ACL().TokenDelete(token.AccessorID, nil)
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
func serviceInstancesForK8SServiceNameAndNamespace(k8sServiceName, k8sServiceNamespace string, client *api.Client) (map[string]*api.AgentService, error) {
	return client.Agent().ServicesWithFilter(
		fmt.Sprintf(`Meta[%q] == %q and Meta[%q] == %q and Meta[%q] == %q`,
			MetaKeyKubeServiceName, k8sServiceName, MetaKeyKubeNS, k8sServiceNamespace, MetaKeyManagedBy, managedByValue))
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

// remoteConsulClient returns an *api.Client that points at the consul agent local to the pod for a provided namespace.
func (r *EndpointsController) remoteConsulClient(ip string, namespace string) (*api.Client, error) {
	newAddr := fmt.Sprintf("%s://%s:%s", r.ConsulScheme, ip, r.ConsulPort)
	localConfig := r.ConsulClientCfg
	localConfig.Address = newAddr
	localConfig.Namespace = namespace
	return consul.NewClient(localConfig, r.ConsulAPITimeout)
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

// filterAgentPods receives meta and object information for Kubernetes resources that are being watched,
// which in this case are Pods. It only returns true if the Pod is a Consul Client Agent Pod. It reads the labels
// from the meta of the resource and uses the values of the "app" and "component" label to validate that
// the Pod is a Consul Client Agent.
func (r *EndpointsController) filterAgentPods(object client.Object) bool {
	podLabels := object.GetLabels()
	app, ok := podLabels["app"]
	if !ok {
		return false
	}
	component, ok := podLabels["component"]
	if !ok {
		return false
	}

	release, ok := podLabels["release"]
	if !ok {
		return false
	}

	if app == "consul" && component == "client" && release == r.ReleaseName {
		return true
	}
	return false
}

// requestsForRunningAgentPods creates a slice of requests for the endpoints controller.
// It enqueues a request for each endpoint that needs to be reconciled. It iterates through
// the list of endpoints and creates a request for those endpoints that have an address that
// are on the same node as the new Consul Agent pod. It receives a Pod Object which is a
// Consul Agent that has been filtered by filterAgentPods and only enqueues endpoints
// for client agent pods where the Ready condition is true.
func (r *EndpointsController) requestsForRunningAgentPods(object client.Object) []ctrl.Request {
	var consulClientPod corev1.Pod
	r.Log.Info("received update for Consul client pod", "name", object.GetName())
	err := r.Client.Get(r.Context, types.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()}, &consulClientPod)
	if k8serrors.IsNotFound(err) {
		// Ignore if consulClientPod is not found.
		return []ctrl.Request{}
	}
	if err != nil {
		r.Log.Error(err, "failed to get Consul client pod", "name", consulClientPod.Name)
		return []ctrl.Request{}
	}
	// We can ignore the agent pod if it's not running, since
	// we can't reconcile and register/deregister services against that agent.
	if consulClientPod.Status.Phase != corev1.PodRunning {
		r.Log.Info("ignoring Consul client pod because it's not running", "name", consulClientPod.Name)
		return []ctrl.Request{}
	}
	// We can ignore the agent pod if it's not yet ready, since
	// we can't reconcile and register/deregister services against that agent.
	for _, cond := range consulClientPod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
			// Ignore if consulClientPod is not ready.
			r.Log.Info("ignoring Consul client pod because it's not ready", "name", consulClientPod.Name)
			return []ctrl.Request{}
		}
	}

	// Get the list of all endpoints.
	var endpointsList corev1.EndpointsList
	err = r.Client.List(r.Context, &endpointsList)
	if err != nil {
		r.Log.Error(err, "failed to list endpoints")
		return []ctrl.Request{}
	}

	// Enqueue requests for endpoints that are on the same node
	// as the client agent.
	var requests []reconcile.Request
	for _, ep := range endpointsList.Items {
		for _, subset := range ep.Subsets {
			allAddresses := subset.Addresses
			allAddresses = append(allAddresses, subset.NotReadyAddresses...)
			for _, address := range allAddresses {
				// Only add requests for the address that is on the same node as the consul client pod.
				if address.NodeName != nil && *address.NodeName == consulClientPod.Spec.NodeName {
					requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: ep.Name, Namespace: ep.Namespace}})
				}
			}
		}
	}
	return requests
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
		if name == getServiceName(pod, serviceEndpoints) {
			return i
		}
	}
	return -1
}
