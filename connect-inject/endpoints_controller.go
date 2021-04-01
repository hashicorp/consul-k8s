package connectinject

import (
	"context"
	"fmt"
	"strings"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	MetaKeyPodName         = "pod-name"
	MetaKeyKubeServiceName = "k8s-service-name"
	MetaKeyKubeNS          = "k8s-namespace"
)

type EndpointsController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	// ConsulScheme is the scheme to use when making API calls to Consul,
	// i.e. "http" or "https".
	ConsulScheme string
	// ConsulPort is the port to make HTTP API calls to Consul agents on.
	ConsulPort string
	// Only endpoints in the AllowK8sNamespacesSet are reconciled.
	AllowK8sNamespacesSet mapset.Set
	// Endpoints in the DenyK8sNamespacesSet are ignored.
	DenyK8sNamespacesSet mapset.Set
	// ReleaseName is the Consul Helm installation release.
	ReleaseName string
	// ReleaseNamespace is the namespace where Consul is installed.
	ReleaseNamespace string
	Log              logr.Logger
	Scheme           *runtime.Scheme
	context.Context

	// GetClientFunc allows us to specify how to get a consul client handle.
	// This is used so that we can provide our own function for testing that is
	// not dependent on having then ENV set up to pick up tokens and ca certs.
	GetClient func(string, string, string) (*api.Client, error)
}

func (r *EndpointsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var serviceEndpoints corev1.Endpoints

	if shouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	err := r.Client.Get(ctx, req.NamespacedName, &serviceEndpoints)

	// If the endpoints object has been deleted (and we get an IsNotFound
	// error), we need to deregister all instances in Consul for that service.
	if k8serrors.IsNotFound(err) {
		// Deregister all instances in Consul for this service. The function deregisterServiceOnAllAgents handles
		// the case where the Consul service name is different from the Kubernetes service name.
		if err = r.deregisterServiceOnAllAgents(ctx, req.Name, req.Namespace, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get Endpoints from Kubernetes", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved Kubernetes Endpoints", "endpoints", serviceEndpoints.Name, "endpoints-namespace", serviceEndpoints.Namespace)

	// endpointAddressMap stores every IP that corresponds to a Pod in the Endpoints object. It is used to compare
	// against service instances in Consul to deregister them if they are not in the map.
	endpointAddressMap := map[string]bool{}

	// Register all addresses of this Endpoints object as service instances in Consul.
	for _, subset := range serviceEndpoints.Subsets {
		// Do the same thing for all addresses, regardless of whether they're ready.
		allAddresses := append(subset.Addresses, subset.NotReadyAddresses...)

		for _, address := range allAddresses {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				// Build the endpointAddressMap up for deregistering service instances later.
				endpointAddressMap[address.IP] = true
				// Get pod associated with this address.
				var pod corev1.Pod
				objectKey := types.NamespacedName{Name: address.TargetRef.Name, Namespace: address.TargetRef.Namespace}
				if err = r.Client.Get(ctx, objectKey, &pod); err != nil {
					r.Log.Error(err, "failed to get pod from Kubernetes", "pod-name", address.TargetRef.Name)
					return ctrl.Result{}, err
				}

				if hasBeenInjected(pod) {
					// Create client for Consul agent local to the pod.
					client, err := r.GetClient(r.ConsulScheme, r.ConsulPort, pod.Status.HostIP)
					if err != nil {
						r.Log.Error(err, "failed to create a new Consul client", "address", pod.Status.HostIP)
						return ctrl.Result{}, err
					}

					// Get information from the pod to create service instance registrations.
					serviceRegistration, proxyServiceRegistration, err := r.createServiceRegistrations(pod, serviceEndpoints)
					if err != nil {
						r.Log.Error(err, "failed to create service registrations", "endpoints", serviceEndpoints.Name)
						return ctrl.Result{}, err
					}

					// Register the service instance with the local agent.
					// Note: the order of how we register services is important,
					// and the connect-proxy service should come after the "main" service
					// because its alias health check depends on the main service existing.
					r.Log.Info("registering service", "service", serviceRegistration.Name)
					err = client.Agent().ServiceRegister(serviceRegistration)
					if err != nil {
						r.Log.Error(err, "failed to register service with Consul", "consul-service-name", serviceRegistration.Name)
						return ctrl.Result{}, err
					}

					// Register the proxy service instance with the local agent.
					r.Log.Info("registering proxy service", "service", proxyServiceRegistration.Name)
					err = client.Agent().ServiceRegister(proxyServiceRegistration)
					if err != nil {
						r.Log.Error(err, "failed to register proxy service with Consul", "consul-proxy-service-name", proxyServiceRegistration.Name)
						return ctrl.Result{}, err
					}
				}
			}
		}
	}

	// Compare service instances in Consul with addresses in Endpoints. If an address is not in Endpoints, deregister
	// from Consul. This uses endpointAddressMap which is populated with the addresses in the Endpoints object during
	// the registration codepath.
	if err = r.deregisterServiceOnAllAgents(ctx, serviceEndpoints.Name, serviceEndpoints.Namespace, endpointAddressMap); err != nil {
		r.Log.Error(err, "failed to deregister service instances on all agents", "k8s-service-name", serviceEndpoints.Name, "k8s-namespace", serviceEndpoints.Namespace)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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

// createServiceRegistrations creates the service and proxy service instance registrations with the information from the
// Pod.
func (r *EndpointsController) createServiceRegistrations(pod corev1.Pod, serviceEndpoints corev1.Endpoints) (*api.AgentServiceRegistration, *api.AgentServiceRegistration, error) {
	// If a port is specified, then we determine the value of that port
	// and register that port for the host service.
	var servicePort int
	if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
		if port, err := portValue(pod, raw); port > 0 {
			if err != nil {
				return nil, nil, err
			}
			servicePort = int(port)
		}
	}

	// TODO: remove logic in handler to always set the service name annotation
	// We only want that annotation to be present when explicitly overriding the consul svc name
	// Otherwise, the Consul service name should equal the Kubernetes Service name.
	// The service name in Consul defaults to the Endpoints object name, and is overridden by the pod
	// annotation consul.hashicorp.com/connect-service..
	serviceName := serviceEndpoints.Name
	if serviceNameFromAnnotation, ok := pod.Annotations[annotationService]; ok && serviceNameFromAnnotation != "" {
		serviceName = serviceNameFromAnnotation
	}

	serviceID := fmt.Sprintf("%s-%s", pod.Name, serviceName)

	meta := map[string]string{
		MetaKeyPodName:         pod.Name,
		MetaKeyKubeServiceName: serviceEndpoints.Name,
		MetaKeyKubeNS:          serviceEndpoints.Namespace,
	}
	for k, v := range pod.Annotations {
		if strings.HasPrefix(k, annotationMeta) && strings.TrimPrefix(k, annotationMeta) != "" {
			meta[strings.TrimPrefix(k, annotationMeta)] = v
		}
	}

	var tags []string
	if raw, ok := pod.Annotations[annotationTags]; ok && raw != "" {
		tags = strings.Split(raw, ",")
	}
	// Get the tags from the deprecated tags annotation and combine.
	if raw, ok := pod.Annotations[annotationConnectTags]; ok && raw != "" {
		tags = append(tags, strings.Split(raw, ",")...)
	}

	service := &api.AgentServiceRegistration{
		ID:        serviceID,
		Name:      serviceName,
		Port:      servicePort,
		Address:   pod.Status.PodIP,
		Meta:      meta,
		Namespace: "", // TODO: namespace support
	}
	if len(tags) > 0 {
		service.Tags = tags
	}

	proxyServiceName := fmt.Sprintf("%s-sidecar-proxy", serviceName)
	proxyServiceID := fmt.Sprintf("%s-%s", pod.Name, proxyServiceName)
	proxyConfig := &api.AgentServiceConnectProxyConfig{
		DestinationServiceName: serviceName,
		DestinationServiceID:   serviceID,
		Config:                 nil, // TODO: add config for metrics (upcoming PR)
	}

	if servicePort > 0 {
		proxyConfig.LocalServiceAddress = "127.0.0.1"
		proxyConfig.LocalServicePort = servicePort
	}

	upstreams, err := r.processUpstreams(pod)
	if err != nil {
		return nil, nil, err
	}
	proxyConfig.Upstreams = upstreams

	proxyService := &api.AgentServiceRegistration{
		Kind:            api.ServiceKindConnectProxy,
		ID:              proxyServiceID,
		Name:            proxyServiceName,
		Port:            20000,
		Address:         pod.Status.PodIP,
		TaggedAddresses: nil, // TODO: set cluster IP here (will be done later)
		Meta:            meta,
		Namespace:       "", // TODO: same as service namespace
		Proxy:           proxyConfig,
		Checks: api.AgentServiceChecks{
			{
				Name:                           "Proxy Public Listener",
				TCP:                            fmt.Sprintf("%s:20000", pod.Status.PodIP),
				Interval:                       "10s",
				DeregisterCriticalServiceAfter: "10m",
			},
			{
				Name:         "Destination Alias",
				AliasService: serviceID,
			},
		},
		Connect: nil,
	}
	if len(tags) > 0 {
		proxyService.Tags = tags
	}

	return service, proxyService, nil
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
	list := corev1.PodList{}
	listOptions := client.ListOptions{
		Namespace: r.ReleaseNamespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"component": "client",
			"app":       "consul",
			"release":   r.ReleaseName,
		}),
	}
	if err := r.Client.List(ctx, &list, &listOptions); err != nil {
		r.Log.Error(err, "failed to get agent pods from Kubernetes")
		return err
	}

	// On each agent, we need to get services matching "k8s-service-name" and "k8s-namespace" metadata.
	for _, pod := range list.Items {
		// Create client for this agent.
		client, err := r.GetClient(r.ConsulScheme, r.ConsulPort, pod.Status.PodIP)
		if err != nil {
			r.Log.Error(err, "failed to create a new Consul client", "address", pod.Status.PodIP)
			return err
		}

		// Get services matching metadata.
		svcs, err := serviceInstancesForK8SServiceNameAndNamespace(k8sSvcName, k8sSvcNamespace, client)
		if err != nil {
			r.Log.Error(err, "failed to get service instances", MetaKeyKubeServiceName, k8sSvcName)
			return err
		}

		// Deregister each service instance that matches the metadata.
		for svcID, serviceRegistration := range svcs {
			// If we selectively deregister, only deregister if the address is not in the map. Otherwise, deregister
			// every service instance.
			if endpointsAddressesMap != nil {
				if _, ok := endpointsAddressesMap[serviceRegistration.Address]; !ok {
					// If the service address is not in the Endpoints addresses, deregister it.
					if err = client.Agent().ServiceDeregister(svcID); err != nil {
						r.Log.Error(err, "failed to deregister service instance", "consul-service-id", svcID)
						return err
					}
				}
			} else {
				if err = client.Agent().ServiceDeregister(svcID); err != nil {
					r.Log.Error(err, "failed to deregister service instance", "consul-service-id", svcID)
					return err
				}
			}
		}
	}
	return nil
}

// serviceInstancesForK8SServiceNameAndNamespace calls Consul's ServicesWithFilter to get the list
// of services instances that have the provided k8sServiceName and k8sServiceNamespace in their metadata.
func serviceInstancesForK8SServiceNameAndNamespace(k8sServiceName, k8sServiceNamespace string, client *api.Client) (map[string]*api.AgentService, error) {
	return client.Agent().ServicesWithFilter(
		fmt.Sprintf(`Meta[%q] == %q and Meta[%q] == %q`,
			MetaKeyKubeServiceName, k8sServiceName, MetaKeyKubeNS, k8sServiceNamespace))
}

// processUpstreams reads the list of upstreams from the Pod annotation and converts them into a list of api.Upstream
// objects.
func (r *EndpointsController) processUpstreams(pod corev1.Pod) ([]api.Upstream, error) {
	var upstreams []api.Upstream
	if raw, ok := pod.Annotations[annotationUpstreams]; ok && raw != "" {
		for _, raw := range strings.Split(raw, ",") {
			parts := strings.SplitN(raw, ":", 3)

			var datacenter, serviceName, preparedQuery string
			var port int32
			if strings.TrimSpace(parts[0]) == "prepared_query" {
				port, _ = portValue(pod, strings.TrimSpace(parts[2]))
				preparedQuery = strings.TrimSpace(parts[1])
			} else {
				port, _ = portValue(pod, strings.TrimSpace(parts[1]))

				// TODO: Parse the namespace if provided

				serviceName = strings.TrimSpace(parts[0])

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
						return []api.Upstream{}, fmt.Errorf("upstream %q is invalid: there is no ProxyDefaults config to set mesh gateway mode", raw)
					} else if err == nil {
						mode := entry.(*api.ProxyConfigEntry).MeshGateway.Mode
						if mode != api.MeshGatewayModeLocal && mode != api.MeshGatewayModeRemote {
							return []api.Upstream{}, fmt.Errorf("upstream %q is invalid: ProxyDefaults mesh gateway mode is neither %q nor %q", raw, api.MeshGatewayModeLocal, api.MeshGatewayModeRemote)
						}
					}
					// NOTE: If we can't reach Consul we don't error out because
					// that would fail the pod scheduling and this is a nice-to-have
					// check, not something that should block during a Consul hiccup.
				}
			}

			if port > 0 {
				upstream := api.Upstream{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationNamespace: "", // todo
					DestinationName:      serviceName,
					Datacenter:           datacenter,
					LocalBindPort:        int(port),
				}

				if preparedQuery != "" {
					upstream.DestinationType = api.UpstreamDestTypePreparedQuery
					upstream.DestinationName = preparedQuery
				}

				upstreams = append(upstreams, upstream)
			}
		}
	}

	return upstreams, nil
}

// GetConsulClient returns an *api.Client that points at the consul agent local to the pod.
func GetConsulClient(scheme, port, ip string) (*api.Client, error) {
	newAddr := fmt.Sprintf("%s://%s:%s", scheme, ip, port)
	localConfig := api.DefaultConfig()
	localConfig.Address = newAddr

	return consul.NewClient(localConfig)
}

// shouldIgnore ignores namespaces where we don't connect-inject.
func shouldIgnore(namespace string, denySet, allowSet mapset.Set) bool {
	// Ignores system namespaces.
	if namespace == metav1.NamespaceSystem || namespace == metav1.NamespacePublic || namespace == "local-path-storage" {
		return true
	}

	// Ignores deny list.
	if denySet.Contains(namespace) {
		fmt.Printf("%+v\n", denySet.ToSlice()...)
		return true
	}

	// Ignores if not in allow list or allow list is not *.
	if !allowSet.Contains("*") && !allowSet.Contains(namespace) {
		fmt.Printf("%+v\n", allowSet.ToSlice()...)
		return true
	}

	return false
}

// filterAgentPods receives meta and object information for Kubernetes resources that are being watched,
// which in this case are Pods. It only returns true if the Pod is a Consul Client Agent Pod. It reads the labels
// from the meta of the resource and uses the values of the "app" and "component" label to validate that
// the Pod is a Consul Client Agent.
func (r EndpointsController) filterAgentPods(object client.Object) bool {
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
func (r EndpointsController) requestsForRunningAgentPods(object client.Object) []ctrl.Request {
	var consulClientPod corev1.Pod
	r.Log.Info("received update for consulClientPod", "podName", object.GetName())
	err := r.Client.Get(r.Context, types.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()}, &consulClientPod)
	if k8serrors.IsNotFound(err) {
		// Ignore if consulClientPod is not found.
		return []ctrl.Request{}
	}
	if err != nil {
		r.Log.Error(err, "failed to get consulClientPod", "consulClientPod", consulClientPod.Name)
		return []ctrl.Request{}
	}
	// We can ignore the agent pod if it's not running, since
	// we can't reconcile and register/deregister services against that agent.
	if consulClientPod.Status.Phase != corev1.PodRunning {
		r.Log.Info("ignoring consulClientPod because it's not running", "consulClientPod", consulClientPod.Name)
		return []ctrl.Request{}
	}
	// We can ignore the agent pod if it's not yet ready, since
	// we can't reconcile and register/deregister services against that agent.
	for _, cond := range consulClientPod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
			// Ignore if consulClientPod is not ready.
			r.Log.Info("ignoring consulClientPod because it's not ready", "consulClientPod", consulClientPod.Name)
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

// hasBeenInjected checks the value of the status annotation and returns true if the Pod has been injected.
func hasBeenInjected(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[annotationStatus]; ok {
		if anno == injected {
			return true
		}
	}
	return false
}
