package connectinject

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	// kubeSystemNamespaces is a set of namespaces that are considered
	// "system" level namespaces and are always skipped (never injected).
	kubeSystemNamespaces = mapset.NewSetWith(metav1.NamespaceSystem, metav1.NamespacePublic)
)

// Handler is the HTTP handler for admission webhooks.
type ConnectWebhook struct {
	ConsulClient *api.Client
	Clientset    kubernetes.Interface

	// ImageConsul is the container image for Consul to use.
	// ImageEnvoy is the container image for Envoy to use.
	//
	// Both of these MUST be set.
	ImageConsul string
	ImageEnvoy  string

	// ImageConsulK8S is the container image for consul-k8s to use.
	// This image is used for the consul-sidecar container.
	ImageConsulK8S string

	// Optional: set when you need extra options to be set when running envoy
	// See a list of args here: https://www.envoyproxy.io/docs/envoy/latest/operations/cli
	EnvoyExtraArgs string

	// RequireAnnotation means that the annotation must be given to inject.
	// If this is false, injection is default.
	RequireAnnotation bool

	// AuthMethod is the name of the Kubernetes Auth Method to
	// use for identity with connectInjection if ACLs are enabled.
	AuthMethod string

	// The PEM-encoded CA certificate string
	// to use when communicating with Consul clients over HTTPS.
	// If not set, will use HTTP.
	ConsulCACert string

	// ConsulPartition is the name of the Admin Partition that the controller
	// is deployed in. It is an enterprise feature requiring Consul Enterprise 1.11+.
	// Its value is an empty string if partitions aren't enabled.
	ConsulPartition string

	// EnableNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which is namespace aware. It enables Consul namespaces,
	// with injection into either a single Consul namespace or mirrored from
	// k8s namespaces.
	EnableNamespaces bool

	// AllowK8sNamespacesSet is a set of k8s namespaces to explicitly allow for
	// injection. It supports the special character `*` which indicates that
	// all k8s namespaces are eligible unless explicitly denied. This filter
	// is applied before checking pod annotations.
	AllowK8sNamespacesSet mapset.Set

	// DenyK8sNamespacesSet is a set of k8s namespaces to explicitly deny
	// injection and thus service registration with Consul. An empty set
	// means that no namespaces are removed from consideration. This filter
	// takes precedence over AllowK8sNamespacesSet.
	DenyK8sNamespacesSet mapset.Set

	// ConsulDestinationNamespace is the name of the Consul namespace to register all
	// injected services into if Consul namespaces are enabled and mirroring
	// is disabled. This may be set, but will not be used if mirroring is enabled.
	ConsulDestinationNamespace string

	// EnableK8SNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any service being registered into Consul. Services are
	// registered into the Consul namespace that mirrors their k8s namespace.
	EnableK8SNSMirroring bool

	// K8SNSMirroringPrefix is an optional prefix that can be added to the Consul
	// namespaces created while mirroring. For example, if it is set to "k8s-",
	// then the k8s `default` namespace will be mirrored in Consul's
	// `k8s-default` namespace.
	K8SNSMirroringPrefix string

	// CrossNamespaceACLPolicy is the name of the ACL policy to attach to
	// any created Consul namespaces to allow cross namespace service discovery.
	// Only necessary if ACLs are enabled.
	CrossNamespaceACLPolicy string

	// Default resource settings for sidecar proxies. Some of these
	// fields may be empty.
	DefaultProxyCPURequest    resource.Quantity
	DefaultProxyCPULimit      resource.Quantity
	DefaultProxyMemoryRequest resource.Quantity
	DefaultProxyMemoryLimit   resource.Quantity

	// MetricsConfig contains metrics configuration from the inject-connect command and has methods to determine whether
	// configuration should come from the default flags or annotations. The handler uses this to configure prometheus
	// annotations and the merged metrics server.
	MetricsConfig MetricsConfig

	// Resource settings for init container. All of these fields
	// will be populated by the defaults provided in the initial flags.
	InitContainerResources corev1.ResourceRequirements

	// Resource settings for Consul sidecar. All of these fields
	// will be populated by the defaults provided in the initial flags.
	DefaultConsulSidecarResources corev1.ResourceRequirements

	// EnableTransparentProxy enables transparent proxy mode.
	// This means that the injected init container will apply traffic redirection rules
	// so that all traffic will go through the Envoy proxy.
	EnableTransparentProxy bool

	// TProxyOverwriteProbes controls whether the webhook should mutate pod's HTTP probes
	// to point them to the Envoy proxy.
	TProxyOverwriteProbes bool

	// EnableConsulDNS enables traffic redirection so that DNS requests are directed to Consul
	// from mesh services.
	EnableConsulDNS bool

	// ResourcePrefix is the prefix used for the installation which is used to determine the Service
	// name of the Consul DNS service.
	ResourcePrefix string

	// EnableOpenShift indicates that when tproxy is enabled, the security context for the Envoy and init
	// containers should not be added because OpenShift sets a random user for those and will not allow
	// those containers to be created otherwise.
	EnableOpenShift bool

	// ConsulAPITimeout is the duration that the consul API client will
	// wait for a response from the API before cancelling the request.
	ConsulAPITimeout time.Duration

	// Log
	Log logr.Logger
	// Log settings for consul-sidecar
	LogLevel string
	LogJSON  bool

	decoder *admission.Decoder
}
type multiPortInfo struct {
	serviceIndex int
	serviceName  string
}

// Handle is the admission.Handler implementation that actually handles the
// webhook request for admission control. This should be registered or
// served via the controller runtime manager.
func (w *ConnectWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var pod corev1.Pod

	// Decode the pod from the request
	if err := w.decoder.Decode(req, &pod); err != nil {
		w.Log.Error(err, "could not unmarshal request to pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Marshall the contents of the pod that was received. This is compared with the
	// marshalled contents of the pod after it has been updated to create the jsonpatch.
	origPodJson, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := w.validatePod(pod); err != nil {
		w.Log.Error(err, "error validating pod", "request name", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Setup the default annotation values that are used for the container.
	// This MUST be done before shouldInject is called since that function
	// uses these annotations.
	if err := w.defaultAnnotations(&pod, string(origPodJson)); err != nil {
		w.Log.Error(err, "error creating default annotations", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error creating default annotations: %s", err))
	}

	// Check if we should inject, for example we don't inject in the
	// system namespaces.
	if shouldInject, err := w.shouldInject(pod, req.Namespace); err != nil {
		w.Log.Error(err, "error checking if should inject", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking if should inject: %s", err))
	} else if !shouldInject {
		return admission.Allowed(fmt.Sprintf("%s %s does not require injection", pod.Kind, pod.Name))
	}

	w.Log.Info("received pod", "name", req.Name, "ns", req.Namespace)

	// Add our volume that will be shared by the init container and
	// the sidecar for passing data in the pod.
	pod.Spec.Volumes = append(pod.Spec.Volumes, w.containerVolume())

	// Optionally mount data volume to other containers
	w.injectVolumeMount(pod)

	// Add the upstream services as environment variables for easy
	// service discovery.
	containerEnvVars := w.containerEnvVars(pod)
	for i := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[i].Env = append(pod.Spec.InitContainers[i].Env, containerEnvVars...)
	}

	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, containerEnvVars...)
	}

	// Add the init container which copies the Consul binary to /consul/connect-inject/.
	initCopyContainer := w.initCopyContainer()
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initCopyContainer)

	// A user can enable/disable tproxy for an entire namespace via a label.
	ns, err := w.Clientset.CoreV1().Namespaces().Get(ctx, req.Namespace, metav1.GetOptions{})
	if err != nil {
		w.Log.Error(err, "error fetching namespace metadata for container", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error getting namespace metadata for container: %s", err))
	}

	// Get service names from the annotation. If theres 0-1 service names, it's a single port pod, otherwise it's multi
	// port.
	annotatedSvcNames := w.annotatedServiceNames(pod)
	multiPort := len(annotatedSvcNames) > 1

	// For single port pods, add the single init container and envoy sidecar.
	if !multiPort {
		// Add the init container that registers the service and sets up the Envoy configuration.
		initContainer, err := w.containerInit(*ns, pod, multiPortInfo{})
		if err != nil {
			w.Log.Error(err, "error configuring injection init container", "request name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection init container: %s", err))
		}
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

		// Add the Envoy sidecar.
		envoySidecar, err := w.envoySidecar(*ns, pod, multiPortInfo{})
		if err != nil {
			w.Log.Error(err, "error configuring injection sidecar container", "request name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection sidecar container: %s", err))
		}
		pod.Spec.Containers = append(pod.Spec.Containers, envoySidecar)
	} else {
		// For multi port pods, check for unsupported cases, mount all relevant service account tokens, and mount an init
		// container and envoy sidecar per port. Tproxy, metrics, and metrics merging are not supported for multi port pods.
		// In a single port pod, the service account specified in the pod is sufficient for mounting the service account
		// token to the pod. In a multi port pod, where multiple services are registered with Consul, we also require a
		// service account per service. So, this will look for service accounts whose name matches the service and mount
		// those tokens if not already specified via the pod's serviceAccountName.

		w.Log.Info("processing multiport pod")
		err := w.checkUnsupportedMultiPortCases(*ns, pod)
		if err != nil {
			w.Log.Error(err, "checking unsupported cases for multi port pods")
			return admission.Errored(http.StatusInternalServerError, err)
		}
		for i, svc := range annotatedSvcNames {
			w.Log.Info(fmt.Sprintf("service: %s", svc))
			if w.AuthMethod != "" {
				if svc != "" && pod.Spec.ServiceAccountName != svc {
					sa, err := w.Clientset.CoreV1().ServiceAccounts(req.Namespace).Get(ctx, svc, metav1.GetOptions{})
					if err != nil {
						w.Log.Error(err, "couldn't get service accounts")
						return admission.Errored(http.StatusInternalServerError, err)
					}
					if len(sa.Secrets) == 0 {
						w.Log.Info(fmt.Sprintf("service account %s has zero secrets exp at least 1", svc))
						return admission.Errored(http.StatusInternalServerError, fmt.Errorf("service account %s has zero secrets, expected at least one", svc))
					}
					saSecret := sa.Secrets[0].Name
					w.Log.Info("found service account, mounting service account secret to Pod", "serviceAccountName", sa.Name)
					pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
						Name: fmt.Sprintf("%s-service-account", svc),
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: saSecret,
							},
						},
					})
				}
			}

			// This will get passed to the init and sidecar containers so they are configured correctly.
			mpi := multiPortInfo{
				serviceIndex: i,
				serviceName:  svc,
			}

			// Add the init container that registers the service and sets up the Envoy configuration.
			initContainer, err := w.containerInit(*ns, pod, mpi)
			if err != nil {
				w.Log.Error(err, "error configuring injection init container", "request name", req.Name)
				return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection init container: %s", err))
			}
			pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

			// Add the Envoy sidecar.
			envoySidecar, err := w.envoySidecar(*ns, pod, mpi)
			if err != nil {
				w.Log.Error(err, "error configuring injection sidecar container", "request name", req.Name)
				return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection sidecar container: %s", err))
			}
			pod.Spec.Containers = append(pod.Spec.Containers, envoySidecar)
		}
	}

	// Now that the consul-sidecar no longer needs to re-register services periodically
	// (that functionality lives in the endpoints-controller),
	// we only need the consul sidecar to run the metrics merging server.
	// First, determine if we need to run the metrics merging server.
	shouldRunMetricsMerging, err := w.MetricsConfig.shouldRunMergedMetricsServer(pod)
	if err != nil {
		w.Log.Error(err, "error determining if metrics merging server should be run", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error determining if metrics merging server should be run: %s", err))
	}

	// Add the consul-sidecar only if we need to run the metrics merging server.
	if shouldRunMetricsMerging {
		consulSidecar, err := w.consulSidecar(pod)
		if err != nil {
			w.Log.Error(err, "error configuring consul sidecar container", "request name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring consul sidecar container: %s", err))
		}
		pod.Spec.Containers = append(pod.Spec.Containers, consulSidecar)
	}

	// pod.Annotations has already been initialized by h.defaultAnnotations()
	// and does not need to be checked for being a nil value.
	pod.Annotations[keyInjectStatus] = injected

	// Add annotations for metrics.
	if err = w.prometheusAnnotations(&pod); err != nil {
		w.Log.Error(err, "error configuring prometheus annotations", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring prometheus annotations: %s", err))
	}

	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[keyInjectStatus] = injected

	// Add the managed-by label since services are now managed by endpoints controller. This is to support upgrading
	// from consul-k8s without Endpoints controller to consul-k8s with Endpoints controller.
	pod.Labels[keyManagedBy] = managedByValue

	// Consul-ENT only: Add the Consul destination namespace as an annotation to the pod.
	if w.EnableNamespaces {
		pod.Annotations[annotationConsulNamespace] = w.consulNamespace(req.Namespace)
	}

	// Overwrite readiness/liveness probes if needed.
	err = w.overwriteProbes(*ns, &pod)
	if err != nil {
		w.Log.Error(err, "error overwriting readiness or liveness probes", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error overwriting readiness or liveness probes: %s", err))
	}

	// Marshall the pod into JSON after it has the desired envs, annotations, labels,
	// sidecars and initContainers appended to it.
	updatedPodJson, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Create a patches based on the Pod that was received by the handler
	// and the desired Pod spec.
	patches, err := jsonpatch.CreatePatch(origPodJson, updatedPodJson)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check and potentially create Consul resources. This is done after
	// all patches are created to guarantee no errors were encountered in
	// that process before modifying the Consul cluster.
	if w.EnableNamespaces {
		if _, err := namespaces.EnsureExists(w.ConsulClient, w.consulNamespace(req.Namespace), w.CrossNamespaceACLPolicy); err != nil {
			w.Log.Error(err, "error checking or creating namespace",
				"ns", w.consulNamespace(req.Namespace), "request name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking or creating namespace: %s", err))
		}
	}

	// Return a Patched response along with the patches we intend on applying to the
	// Pod received by the handler.
	return admission.Patched(fmt.Sprintf("valid %s request", pod.Kind), patches...)
}

// shouldOverwriteProbes returns true if we need to overwrite readiness/liveness probes for this pod.
// It returns an error when the annotation value cannot be parsed by strconv.ParseBool.
func shouldOverwriteProbes(pod corev1.Pod, globalOverwrite bool) (bool, error) {
	if raw, ok := pod.Annotations[annotationTransparentProxyOverwriteProbes]; ok {
		return strconv.ParseBool(raw)
	}

	return globalOverwrite, nil
}

// overwriteProbes overwrites readiness/liveness probes of this pod when
// both transparent proxy is enabled and overwrite probes is true for the pod.
func (w *ConnectWebhook) overwriteProbes(ns corev1.Namespace, pod *corev1.Pod) error {
	tproxyEnabled, err := transparentProxyEnabled(ns, *pod, w.EnableTransparentProxy)
	if err != nil {
		return err
	}

	overwriteProbes, err := shouldOverwriteProbes(*pod, w.TProxyOverwriteProbes)
	if err != nil {
		return err
	}

	if tproxyEnabled && overwriteProbes {
		for i, container := range pod.Spec.Containers {
			// skip the "envoy-sidecar" container from having it's probes overridden
			if container.Name == envoySidecarContainer {
				continue
			}
			if container.LivenessProbe != nil && container.LivenessProbe.HTTPGet != nil {
				container.LivenessProbe.HTTPGet.Port = intstr.FromInt(exposedPathsLivenessPortsRangeStart + i)
			}
			if container.ReadinessProbe != nil && container.ReadinessProbe.HTTPGet != nil {
				container.ReadinessProbe.HTTPGet.Port = intstr.FromInt(exposedPathsReadinessPortsRangeStart + i)
			}
			if container.StartupProbe != nil && container.StartupProbe.HTTPGet != nil {
				container.StartupProbe.HTTPGet.Port = intstr.FromInt(exposedPathsStartupPortsRangeStart + i)
			}
		}
	}
	return nil
}

func (w *ConnectWebhook) injectVolumeMount(pod corev1.Pod) {
	containersToInject := splitCommaSeparatedItemsFromAnnotation(annotationInjectMountVolumes, pod)

	for index, container := range pod.Spec.Containers {
		if sliceContains(containersToInject, container.Name) {
			pod.Spec.Containers[index].VolumeMounts = append(pod.Spec.Containers[index].VolumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			})
		}
	}
}

func (w *ConnectWebhook) shouldInject(pod corev1.Pod, namespace string) (bool, error) {
	// Don't inject in the Kubernetes system namespaces
	if kubeSystemNamespaces.Contains(namespace) {
		return false, nil
	}

	// Namespace logic
	// If in deny list, don't inject
	if w.DenyK8sNamespacesSet.Contains(namespace) {
		return false, nil
	}

	// If not in allow list or allow list is not *, don't inject
	if !w.AllowK8sNamespacesSet.Contains("*") && !w.AllowK8sNamespacesSet.Contains(namespace) {
		return false, nil
	}

	// If we already injected then don't inject again
	if pod.Annotations[keyInjectStatus] != "" {
		return false, nil
	}

	// If the explicit true/false is on, then take that value. Note that
	// this has to be the last check since it sets a default value after
	// all other checks.
	if raw, ok := pod.Annotations[annotationInject]; ok {
		return strconv.ParseBool(raw)
	}

	return !w.RequireAnnotation, nil
}

func (w *ConnectWebhook) defaultAnnotations(pod *corev1.Pod, podJson string) error {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	// Default service port is the first port exported in the container
	if _, ok := pod.ObjectMeta.Annotations[annotationPort]; !ok {
		if cs := pod.Spec.Containers; len(cs) > 0 {
			if ps := cs[0].Ports; len(ps) > 0 {
				if ps[0].Name != "" {
					pod.Annotations[annotationPort] = ps[0].Name
				} else {
					pod.Annotations[annotationPort] = strconv.Itoa(int(ps[0].ContainerPort))
				}
			}
		}
	}
	pod.Annotations[annotationOriginalPod] = podJson

	return nil
}

// prometheusAnnotations sets the Prometheus scraping configuration
// annotations on the Pod.
func (w *ConnectWebhook) prometheusAnnotations(pod *corev1.Pod) error {
	enableMetrics, err := w.MetricsConfig.enableMetrics(*pod)
	if err != nil {
		return err
	}
	prometheusScrapePort, err := w.MetricsConfig.prometheusScrapePort(*pod)
	if err != nil {
		return err
	}
	prometheusScrapePath := w.MetricsConfig.prometheusScrapePath(*pod)

	if enableMetrics {
		pod.Annotations[annotationPrometheusScrape] = "true"
		pod.Annotations[annotationPrometheusPort] = prometheusScrapePort
		pod.Annotations[annotationPrometheusPath] = prometheusScrapePath
	}
	return nil
}

// consulNamespace returns the namespace that a service should be
// registered in based on the namespace options. It returns an
// empty string if namespaces aren't enabled.
func (w *ConnectWebhook) consulNamespace(ns string) string {
	return namespaces.ConsulNamespace(ns, w.EnableNamespaces, w.ConsulDestinationNamespace, w.EnableK8SNSMirroring, w.K8SNSMirroringPrefix)
}

func (w *ConnectWebhook) validatePod(pod corev1.Pod) error {
	if _, ok := pod.Annotations[annotationProtocol]; ok {
		return fmt.Errorf("the %q annotation is no longer supported. Instead, create a ServiceDefaults resource (see www.consul.io/docs/k8s/crds/upgrade-to-crds)",
			annotationProtocol)
	}

	if _, ok := pod.Annotations[annotationSyncPeriod]; ok {
		return fmt.Errorf("the %q annotation is no longer supported because consul-sidecar is no longer injected to periodically register services", annotationSyncPeriod)
	}
	return nil
}

func portValue(pod corev1.Pod, value string) (int32, error) {
	value = strings.Split(value, ",")[0]
	// First search for the named port.
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			if p.Name == value {
				return p.ContainerPort, nil
			}
		}
	}

	// Named port not found, return the parsed value.
	raw, err := strconv.ParseInt(value, 0, 32)
	return int32(raw), err
}

func findServiceAccountVolumeMount(pod corev1.Pod, multiPort bool, multiPortSvcName string) (corev1.VolumeMount, string, error) {
	// In the case of a multiPort pod, there may be another service account
	// token mounted as a different volume. Its name must be <svc>-serviceaccount.
	// If not we'll fall back to the service account for the pod.
	if multiPort {
		for _, v := range pod.Spec.Volumes {
			if v.Name == fmt.Sprintf("%s-service-account", multiPortSvcName) {
				mountPath := fmt.Sprintf("/consul/serviceaccount-%s", multiPortSvcName)
				return corev1.VolumeMount{
					Name:      v.Name,
					ReadOnly:  true,
					MountPath: mountPath,
				}, filepath.Join(mountPath, "token"), nil
			}
		}
	}

	// Find the volume mount that is mounted at the known
	// service account token location
	var volumeMount corev1.VolumeMount
	for _, container := range pod.Spec.Containers {
		for _, vm := range container.VolumeMounts {
			if vm.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount" {
				volumeMount = vm
				break
			}
		}
	}

	// Return an error if volumeMount is still empty
	if (corev1.VolumeMount{}) == volumeMount {
		return volumeMount, "", errors.New("unable to find service account token volumeMount")
	}

	return volumeMount, "/var/run/secrets/kubernetes.io/serviceaccount/token", nil
}

func (w *ConnectWebhook) annotatedServiceNames(pod corev1.Pod) []string {
	var annotatedSvcNames []string
	if anno, ok := pod.Annotations[annotationService]; ok {
		annotatedSvcNames = strings.Split(anno, ",")
	}
	return annotatedSvcNames
}

func (w *ConnectWebhook) checkUnsupportedMultiPortCases(ns corev1.Namespace, pod corev1.Pod) error {
	tproxyEnabled, err := transparentProxyEnabled(ns, pod, w.EnableTransparentProxy)
	if err != nil {
		return fmt.Errorf("couldn't check if tproxy is enabled: %s", err)
	}
	metricsEnabled, err := w.MetricsConfig.enableMetrics(pod)
	if err != nil {
		return fmt.Errorf("couldn't check if metrics is enabled: %s", err)
	}
	metricsMergingEnabled, err := w.MetricsConfig.enableMetricsMerging(pod)
	if err != nil {
		return fmt.Errorf("couldn't check if metrics merging is enabled: %s", err)
	}
	if tproxyEnabled {
		return fmt.Errorf("multi port services are not compatible with transparent proxy")
	}
	if metricsEnabled {
		return fmt.Errorf("multi port services are not compatible with metrics")
	}
	if metricsMergingEnabled {
		return fmt.Errorf("multi port services are not compatible with metrics merging")
	}
	return nil
}

func (w *ConnectWebhook) InjectDecoder(d *admission.Decoder) error {
	w.decoder = d
	return nil
}

func sliceContains(slice []string, entry string) bool {
	for _, s := range slice {
		if entry == s {
			return true
		}
	}
	return false
}
