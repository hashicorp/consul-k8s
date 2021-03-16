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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// todo: add docs
type EndpointsController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod
	ConsulClient *api.Client
	// ConsulScheme is the scheme to use when making API calls to Consul,
	// i.e. "http" or "https".
	ConsulScheme string
	// ConsulPort is the port to make HTTP API calls to Consul agents on.
	ConsulPort            string
	AllowK8sNamespacesSet mapset.Set
	DenyK8sNamespacesSet  mapset.Set
	Log                   logr.Logger
	Scheme                *runtime.Scheme
}

// TODO: get consul installation namespace passed in for querying agents (for more efficient lookup of agent pods)

const MetaKeyKubeServiceName = "k8s-service-name"

func (r *EndpointsController) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var serviceEndpoints corev1.Endpoints

	// Ignores system namespaces.
	if req.Namespace == "kube-system" || req.Namespace == "local-path-storage" {
		return ctrl.Result{}, nil
	}

	// Ignore namespaces where we don't connect-inject.
	// Ignores deny list.
	if r.DenyK8sNamespacesSet.Contains(req.Namespace) {
		fmt.Printf("%+v\n", r.DenyK8sNamespacesSet.ToSlice()...)
		return ctrl.Result{}, nil
	}
	// Ignores if not in allow list or allow list is not *.
	if !r.AllowK8sNamespacesSet.Contains("*") && !r.AllowK8sNamespacesSet.Contains(req.Namespace) {
		fmt.Printf("%+v\n", r.AllowK8sNamespacesSet.ToSlice()...)
		return ctrl.Result{}, nil
	}

	err := r.Client.Get(context.Background(), req.NamespacedName, &serviceEndpoints)

	// If the endpoints object has been deleted (and we get an IsNotFound
	// error), we need to deregister all instances in Consul for that service.
	if k8serrors.IsNotFound(err) {
		// The k8s service name may or may not match the consul service
		// name, but the k8s service name will always match the metadata on
		// the Consul service "k8s-service-name". So, we query Consul
		// services by "k8s-service-name" metadata, which is only exposed on
		// the agent API. Therefore, we need to query all agents who have
		// services matching that metadata, and deregister each service
		// instance.
		err := r.deregisterServiceOnAllAgents(req.Name, req.Namespace)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get endpoints from Kubernetes", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved service from kube", "serviceEndpoints", serviceEndpoints)

	// Get Consul service name for the Endpoints object
	// Compare service instances in Consul with addresses in Endpoints. If an address is not in Endpoints,
	// deregister from Consul.

	// The name of the service in Consul will either be the name of the Endpoints object,
	// or the annotationService on the first Pod referenced
	var serviceNameForDeregistrationCheck string
	endpointAddressMap := map[string]bool{}

	// Register all addresses of this Endpoints object as service instances in Consul.
	for i, subset := range serviceEndpoints.Subsets {
		// Do the same thing for all addresses, regardless of whether they're ready.
		allAddresses := subset.Addresses
		allAddresses = append(allAddresses, subset.NotReadyAddresses...)

		r.Log.Info("all addresses", "addresses", allAddresses)
		for j, address := range allAddresses {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				// Build the endpointAddressMap up for deregistering service instances later.
				endpointAddressMap[address.IP] = true
				// Get pod associated with this address.
				var pod corev1.Pod
				objectKey := types.NamespacedName{Name: address.TargetRef.Name, Namespace: address.TargetRef.Namespace}
				err = r.Client.Get(context.Background(), objectKey, &pod)
				if err != nil {
					r.Log.Error(err, "failed to get pod from Kubernetes", "pod name", address.TargetRef.Name)
					return ctrl.Result{}, err
				}

				if hasBeenInjected(&pod) {
					// Create client for Consul agent local to the pod.
					client, err := r.getConsulClient(pod.Status.HostIP)
					if err != nil {
						r.Log.Error(err, "failed to create a new Consul client", "address", pod.Status.HostIP)
						return ctrl.Result{}, err
					}

					// If a port is specified, then we determine the value of that port
					// and register that port for the host service.
					var servicePort int
					if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
						if port, _ := portValue(&pod, raw); port > 0 {
							servicePort = int(port)
						}
					}

					// TODO remove logic in handler to always set the service name annotation
					// We only want that annotation to be present when explicitly overriding the consul svc name
					// Otherwise, the Consul service name should equal the K8s Service name.
					var serviceName string
					serviceName = serviceEndpoints.Name
					if raw, ok := pod.Annotations[annotationService]; ok && raw != "" {
						serviceName = raw
					}
					if i == 0 && j == 0 {
						serviceNameForDeregistrationCheck = serviceName
					}

					serviceID := fmt.Sprintf("%s-%s", pod.Name, serviceName)
					// TODO tags, meta, upstreams

					//fmt.Printf("&&& Pod name: %+v, service port: %+v, service name: %+v, service id: %+v\n", pod, servicePort, serviceName, serviceID)
					service := &api.AgentServiceRegistration{
						ID:      serviceID,
						Name:    serviceName,
						Tags:    nil, // todo: process tags from annotations
						Port:    servicePort,
						Address: pod.Status.PodIP,
						Meta: map[string]string{MetaKeyPodName: pod.Name,
							MetaKeyKubeServiceName: serviceEndpoints.Name,
							MetaKeyKubeNS:          serviceEndpoints.Namespace}, // todo process user-provided meta tag;
						Namespace: "", // todo deal with namespaces
					}
					r.Log.Info("registering service", "service", service)
					err = client.Agent().ServiceRegister(service)
					if err != nil {
						r.Log.Error(err, "failed to register service with Consul", "service name", service.Name)
						return ctrl.Result{}, err
					}

					proxyServiceName := fmt.Sprintf("%s-sidecar-proxy", serviceName)
					proxyServiceID := fmt.Sprintf("%s-%s", pod.Name, proxyServiceName)
					proxyConfig := &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: serviceName,
						DestinationServiceID:   serviceID,
						Config:                 nil, // todo: add config for metrics
					}

					if servicePort > 0 {
						proxyConfig.LocalServiceAddress = "127.0.0.1"
						proxyConfig.LocalServicePort = servicePort
					}

					proxyConfig.Upstreams, err = r.processUpstreams(&pod)
					if err != nil {
						return ctrl.Result{}, err
					}

					proxyService := &api.AgentServiceRegistration{
						Kind:            api.ServiceKindConnectProxy,
						ID:              proxyServiceID,
						Name:            proxyServiceName,
						Tags:            nil, // todo: same as service tags
						Port:            20000,
						Address:         pod.Status.PodIP,
						TaggedAddresses: nil,                                                                                                                                   // todo: set cluster IP here (will be done later)
						Meta:            map[string]string{MetaKeyPodName: pod.Name, MetaKeyKubeServiceName: serviceEndpoints.Name, MetaKeyKubeNS: serviceEndpoints.Namespace}, // todo process user-provided meta tag;
						Namespace:       "",                                                                                                                                    // todo: same as service namespace
						Proxy:           proxyConfig,
						Check:           nil,
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

					r.Log.Info("registering proxy service", "service", proxyService)
					err = client.Agent().ServiceRegister(proxyService)
					if err != nil {
						r.Log.Error(err, "failed to register proxy service with Consul", "service name", proxyServiceName)
						return ctrl.Result{}, err
					}

				}
			}
		}
		// Deregister service instances in Consul that are no longer in the Endpoints address.
		// This is done by getting all instances for this service from Consul, comparing
		// allAddresses with the instances. For each service instance, if it is not in
		// allAddresses, it is deregistered.

		// Create a map from allAddresses, so we can check if the instance address is in this map.
		//ipMap := map[string]bool{}
		//for _, epAddress := range allAddresses {
		//	ipMap[epAddress.IP] = true
		//}
		//
		//fmt.Printf("==== ips in eps obj %+v\n", ipMap)
		//// Deregisters the service and proxy service instances if they are not in the list of
		//// allAddresses.
		//err := r.deregisterServiceOnAllAgentsIfNotInMap(req.Name, req.Namespace, ipMap)
		//if err != nil {
		//	r.Log.Info("failed to deregister instances not in map", "service", req.Name)
		//	return ctrl.Result{}, err
		//}
		//serviceInstances, _, err := r.ConsulClient.Catalog().Service(req.Name, "", nil)
		//fmt.Printf("new svcinstances %+v", serviceInstances)
	}

	// Deregister service instances in Consul that are no longer in the Endpoints addresses.
	// TODO only do this if there is a serviceNameForDeregistration
	fmt.Printf("*** serviceNameForDeregistration %s\n", serviceNameForDeregistrationCheck)
	serviceInstances, _, err := r.ConsulClient.Catalog().Service(serviceNameForDeregistrationCheck, "", nil)
	proxyInstances, _, err := r.ConsulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", serviceNameForDeregistrationCheck), "", nil)
	fmt.Printf("*** svcinstances %+v\n", serviceInstances)
	fmt.Printf("*** endpointAddressMap %+v\n", endpointAddressMap)

	serviceAndProxyInstances := append(serviceInstances, proxyInstances...)

	for _, instance := range serviceAndProxyInstances {
		fmt.Printf("*** instance %+v\n", instance)
		if _, ok := endpointAddressMap[instance.ServiceAddress]; !ok {

			agentClient, err := r.getConsulClient(instance.Address)
			if err != nil {
				r.Log.Error(err, "failed to create a new Consul client", "address", instance.Address)
				return ctrl.Result{}, err
			}

			err = agentClient.Agent().ServiceDeregister(instance.ServiceID)
			if err != nil {
				r.Log.Error(err, "failed to deregister service instance", "service", serviceNameForDeregistrationCheck, "serviceID", instance.ServiceID)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// When querying by MetaKeyKubeServiceName, the request will return service instances and associated proxy service instances.
// TODO pass in a context for entire reconcile, not context.Background
func (r *EndpointsController) deregisterServiceOnAllAgents(k8sSvcName, k8sSvcNamespace string) error {

	// Get all agents by getting pods with label component=client
	// TODO more strict: app=consul, maybe release name (need to pass in), also namespace
	list := corev1.PodList{}
	listOptions := client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"component": "client"}),
	}
	// TODO error check
	r.Client.List(context.Background(), &list, &listOptions)

	// On each agent, we need to get services matching "k8s-service-name" and "k8s-namespace" metadata.
	for _, pod := range list.Items {
		// Create client for this agent.
		client, err := r.getConsulClient(pod.Status.PodIP)
		if err != nil {
			r.Log.Error(err, "failed to create a new Consul client", "address", pod.Status.PodIP)
			return err
		}

		// Get services matching metadata.
		svcs, err := client.Agent().ServicesWithFilter(fmt.Sprintf(`Meta[%q] == %q and Meta[%q] == %q`, MetaKeyKubeServiceName, k8sSvcName, MetaKeyKubeNS, k8sSvcNamespace))
		if err != nil {
			r.Log.Error(err, "failed to get service instances", MetaKeyKubeServiceName, k8sSvcName)
			return err
		}

		// Deregister each service instance that matches the metadata
		for svcID, _ := range svcs {
			r.Log.Info("deregistering service", "service id", svcID)
			err = client.Agent().ServiceDeregister(svcID)
			if err != nil {
				r.Log.Error(err, "failed to deregister service instance", "ID", svcID)
				return err
			}
		}

	}
	return nil
}

// When querying by MetaKeyKubeServiceName, the request will return service instances and associated proxy service instances.
// TODO: use the catalog api by deriving the Consul service name from a Pod in the Endpoints object ***
func (r *EndpointsController) deregisterServiceOnAllAgentsIfNotInMap(k8sSvcName, k8sSvcNamespace string, ipMap map[string]bool) error {
	// Get all agents by getting pods with label component=client
	list := corev1.PodList{}
	listOptions := client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"component": "client"}),
	}
	r.Client.List(context.Background(), &list, &listOptions)

	// On each agent, we need to get services matching "k8s-service-name" and "k8s-namespace" metadata.
	for _, pod := range list.Items {
		// Create client for this agent.
		agentClient, err := r.getConsulClient(pod.Status.PodIP)
		if err != nil {
			r.Log.Error(err, "failed to create a new Consul client", "address", pod.Status.PodIP)
			return err
		}

		// Get services matching metadata.
		// TODO svc ids might only be unique by agent
		svcs, err := agentClient.Agent().ServicesWithFilter(fmt.Sprintf(`Meta[%q] == %q and Meta[%q] == %q`, MetaKeyKubeServiceName, k8sSvcName, MetaKeyKubeNS, k8sSvcNamespace))
		fmt.Printf("==== svc instances %+v\n", svcs)
		if err != nil {
			r.Log.Error(err, "failed to get service instances", MetaKeyKubeServiceName, k8sSvcName)
			return err
		}

		// For each service instance, if it's not in the map of addresses for this endpoints object, deregister the
		// service instance.
		for svcID, instance := range svcs {
			fmt.Printf("&&&&&&& instance: %+v, ipmap[instance.svcaddr]: %+v\n", instance, ipMap)
			// if the service instance is in Consul and not in the endpoint
			if _, ok := ipMap[instance.Address]; !ok {
				fmt.Printf("==== instance was not in ipmap\n")
				err = agentClient.Agent().ServiceDeregister(svcID)
				if err != nil {
					r.Log.Error(err, "failed to deregister service instance", "ID", svcID)
					return err
				}
				fmt.Printf("should have deregistered\n")
			}
		}
	}
	return nil
}

func (r *EndpointsController) processUpstreams(pod *corev1.Pod) ([]api.Upstream, error) {
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

				// todo: Parse the namespace if provided
				//if data.ConsulNamespace != "" {
				//	pieces := strings.SplitN(parts[0], ".", 2)
				//	serviceName = pieces[0]
				//
				//	if len(pieces) > 1 {
				//		namespace = pieces[1]
				//	}
				//} else {
				//	serviceName = strings.TrimSpace(parts[0])
				//}

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

func hasBeenInjected(pod *corev1.Pod) bool {
	if anno, ok := pod.Annotations[annotationStatus]; ok {
		if anno == injected {
			return true
		}
	}
	return false
}

// getConsulClient returns an *api.Client that points at the consul agent local to the pod.
func (r *EndpointsController) getConsulClient(ip string) (*api.Client, error) {
	// todo: un-hardcode the scheme and port
	newAddr := fmt.Sprintf("%s://%s:%s", r.ConsulScheme, ip, r.ConsulPort)
	localConfig := api.DefaultConfig()
	localConfig.Address = newAddr

	localClient, err := consul.NewClient(localConfig)
	if err != nil {
		return nil, err
	}

	return localClient, err
}

func (r *EndpointsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *EndpointsController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(r)
}

// NOTES
//
// The following can work for when k8s svc == consul svc. We can add this as an optimization to above.
// ---------------------------------------------------------------------------------------
// then deregister each service instance [is there a way to deregister the whole service]
// below it's done by each svc instance
// r.ConsulClient.Catalog().Services(q *api.QueryOptions) --> name of svcs

// Use this path for if k8s service == consul service
// serviceInstances, _, err := r.ConsulClient.Catalog().Service(name, "", nil)
// if err != nil {
// 	r.Log.Error(err, "failed to get service instances from Consul", "name", name)
// 	return ctrl.Result{}, err
// }
// for _, instance := range serviceInstances {
// 	agentClient, err := r.getConsulClient(instance.Address) // this is the pod IP of the consul client agent rather than service address
// 	if err != nil {
// 		r.Log.Error(err, "failed to create a new Consul client", "address", instance.Address)
// 		return ctrl.Result{}, err
// 	}
// 	r.Log.Info("deregistering service", "service", instance.ServiceName)
// 	err = agentClient.Agent().ServiceDeregister(instance.ServiceID)
// 	if err != nil {
// 		r.Log.Error(err, "failed to deregister service", "name", name)
// 		return ctrl.Result{}, err
// 	}
// }
