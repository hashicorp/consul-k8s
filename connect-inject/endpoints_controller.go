package connectinject

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// todo
type EndpointsController struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *EndpointsController) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var serviceEndpoints corev1.Endpoints

	err := r.Client.Get(context.Background(), req.NamespacedName, &serviceEndpoints)
	// todo: if not found we should deregister all instances
	if err != nil {
		panic(err)
	}
	r.Log.Info("retrieved service from kube", "serviceEndpoints", serviceEndpoints)

	// todo: right now we are just looking at new service endpoints in kube that are not in consul
	// we'd also need to reconcile existing endpoints
	for _, subset := range serviceEndpoints.Subsets {
		// Do the same thing for all addresses, regardless of whether they're ready
		allAddresses := subset.Addresses
		allAddresses = append(allAddresses, subset.NotReadyAddresses...)

		for _, address := range subset.Addresses {
			if address.TargetRef.Kind == "Pod" {
				var pod corev1.Pod
				objectKey := types.NamespacedName{Name: address.TargetRef.Name, Namespace: address.TargetRef.Namespace}
				err = r.Client.Get(context.Background(), objectKey, &pod)
				if err != nil {
					panic(err)
				}

				willBeInjected, err := r.willBeInjected(&pod, req.Namespace)
				if err != nil {
					panic(err)
				}

				if willBeInjected {
					// get consul client
					client, err := getConsulClient(&pod)
					if err != nil {
						panic(err)
					}
					// If a port is specified, then we determine the value of that port
					// and register that port for the host service.
					var servicePort int
					if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
						if port, _ := portValue(&pod, raw); port > 0 {
							servicePort = int(port)
						}
					}
					serviceID := fmt.Sprintf("%s-%s", pod.Name, serviceEndpoints.Name)
					service := &api.AgentServiceRegistration{
						ID:        "",
						Name:      serviceEndpoints.Name,
						Tags:      nil, // todo
						Port:      servicePort,
						Address:   pod.Status.PodIP,
						Meta:      map[string]string{"pod_name": pod.Name}, // todo process user-provided meta tag
						Namespace: "",                                      // todo deal with namespaces
					}
					err = client.Agent().ServiceRegister(service)
					if err != nil {
						panic(err)
					}

					proxyServiceName := fmt.Sprintf("%s-sidecar-proxy", serviceEndpoints.Name)
					proxyServiceID := fmt.Sprintf("%s-%s", pod.Name, proxyServiceName)
					proxyConfig := &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: serviceEndpoints.Name,
						DestinationServiceID:   serviceID,
						Config:                 nil,
						Upstreams:              nil, // todo: deal with upstreams
						MeshGateway:            api.MeshGatewayConfig{},
						Expose:                 api.ExposeConfig{},
					}
					if servicePort > 0 {
						proxyConfig.LocalServiceAddress = "127.0.0.1"
						proxyConfig.LocalServicePort = servicePort
					}
					proxyService := &api.AgentServiceRegistration{
						Kind:            api.ServiceKindConnectProxy,
						ID:              proxyServiceID,
						Name:            proxyServiceName,
						Tags:            nil, // todo: same as service tags
						Port:            20000,
						Address:         pod.Status.PodIP,
						TaggedAddresses: nil,                                     // todo: set cluster IP here
						Meta:            map[string]string{"pod_name": pod.Name}, // todo: same as service meta
						Namespace:       "",                                      // todo: same as service namespace
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: serviceEndpoints.Name,
							DestinationServiceID:   serviceID,
							LocalServiceAddress:    "127",
							LocalServicePort:       0,
							Config:                 nil,
							Upstreams:              nil,
							MeshGateway:            api.MeshGatewayConfig{},
							Expose:                 api.ExposeConfig{},
						},
						Check: nil,
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

					err = client.Agent().ServiceRegister(proxyService)
					if err != nil {
						panic(err)
					}
				}

			}
		}
	}

	return ctrl.Result{}, nil
}

// todo: we don't need this actually - we can just use the connectInject label that hc contoller is using
func (r *EndpointsController) willBeInjected(pod *corev1.Pod, namespace string) (bool, error) {
	// Don't inject in the Kubernetes system namespaces
	if kubeSystemNamespaces.Contains(namespace) {
		return false, nil
	}

	// todo
	// Namespace logic
	// If in deny list, don't inject
	//if h.DenyK8sNamespacesSet.Contains(namespace) {
	//	return false, nil
	//}

	// todo
	// If not in allow list or allow list is not *, don't inject
	//if !h.AllowK8sNamespacesSet.Contains("*") && !h.AllowK8sNamespacesSet.Contains(namespace) {
	//	return false, nil
	//}

	// If we already injected then don't inject again
	if pod.Annotations[annotationStatus] != "" {
		return false, nil
	}

	// A service name is required. Whether a proxy accepting connections
	// or just establishing outbound, a service name is required to acquire
	// the correct certificate.
	if pod.Annotations[annotationService] == "" {
		return false, nil
	}

	// If the explicit true/false is on, then take that value. Note that
	// this has to be the last check since it sets a default value after
	// all other checks.
	if raw, ok := pod.Annotations[AnnotationInject]; ok {
		return strconv.ParseBool(raw)
	}

	// todo: pass require annotation to the endpoints controller
	requireAnnotation := false

	return !requireAnnotation, nil
}

// getConsulClient returns an *api.Client that points at the consul agent local to the pod.
func getConsulClient(pod *corev1.Pod) (*api.Client, error) {
	// todo: un-hardcode the scheme and port
	newAddr := fmt.Sprintf("%s://%s:%s", "http", pod.Status.HostIP, "8500")
	localConfig := api.DefaultConfig()
	localConfig.Address = newAddr

	if pod.Annotations[annotationConsulNamespace] != "" {
		localConfig.Namespace = pod.Annotations[annotationConsulNamespace]
	}

	localClient, err := api.NewClient(localConfig)
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
		For(&corev1.EndpointsList{}).
		Complete(r)
}
