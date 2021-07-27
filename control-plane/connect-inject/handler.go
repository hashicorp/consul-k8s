package connectinject

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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
type Handler struct {
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
	ConsulSidecarResources corev1.ResourceRequirements

	// EnableTransparentProxy enables transparent proxy mode.
	// This means that the injected init container will apply traffic redirection rules
	// so that all traffic will go through the Envoy proxy.
	EnableTransparentProxy bool

	// TProxyOverwriteProbes controls whether the webhook should mutate pod's HTTP probes
	// to point them to the Envoy proxy.
	TProxyOverwriteProbes bool

	// EnableOpenShift indicates that when tproxy is enabled, the security context for the Envoy and init
	// containers should not be added because OpenShift sets a random user for those and will not allow
	// those containers to be created otherwise.
	EnableOpenShift bool

	// Log
	Log logr.Logger
	// Log settings for consul-sidecar
	LogLevel string
	LogJSON  bool

	decoder *admission.Decoder
}

// Handle is the admission.Handler implementation that actually handles the
// webhook request for admission control. This should be registered or
// served via the controller runtime manager.
func (h *Handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	var pod corev1.Pod

	// Decode the pod from the request
	if err := h.decoder.Decode(req, &pod); err != nil {
		h.Log.Error(err, "could not unmarshal request to pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Marshall the contents of the pod that was received. This is compared with the
	// marshalled contents of the pod after it has been updated to create the jsonpatch.
	origPodJson, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := h.validatePod(pod); err != nil {
		h.Log.Error(err, "error validating pod", "request name", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Setup the default annotation values that are used for the container.
	// This MUST be done before shouldInject is called since that function
	// uses these annotations.
	if err := h.defaultAnnotations(&pod, string(origPodJson)); err != nil {
		h.Log.Error(err, "error creating default annotations", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error creating default annotations: %s", err))
	}

	// Check if we should inject, for example we don't inject in the
	// system namespaces.
	if shouldInject, err := h.shouldInject(pod, req.Namespace); err != nil {
		h.Log.Error(err, "error checking if should inject", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking if should inject: %s", err))
	} else if !shouldInject {
		return admission.Allowed(fmt.Sprintf("%s %s does not require injection", pod.Kind, pod.Name))
	}

	h.Log.Info("received pod", "name", req.Name, "ns", req.Namespace)

	// Add our volume that will be shared by the init container and
	// the sidecar for passing data in the pod.
	pod.Spec.Volumes = append(pod.Spec.Volumes, h.containerVolume())

	// Add the upstream services as environment variables for easy
	// service discovery.
	containerEnvVars := h.containerEnvVars(pod)
	for i := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[i].Env = append(pod.Spec.InitContainers[i].Env, containerEnvVars...)
	}

	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, containerEnvVars...)
	}

	// Add the init container which copies the Consul binary to /consul/connect-inject/.
	initCopyContainer := h.initCopyContainer()
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initCopyContainer)

	// A user can enable/disable tproxy for an entire namespace via a label.
	ns, err := h.Clientset.CoreV1().Namespaces().Get(ctx, req.Namespace, metav1.GetOptions{})
	if err != nil {
		h.Log.Error(err, "error fetching namespace metadata for container", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error getting namespace metadata for container: %s", err))
	}

	// Add the init container that registers the service and sets up the Envoy configuration.
	initContainer, err := h.containerInit(*ns, pod)
	if err != nil {
		h.Log.Error(err, "error configuring injection init container", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection init container: %s", err))
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

	// Add the Envoy sidecar.
	envoySidecar, err := h.envoySidecar(*ns, pod)
	if err != nil {
		h.Log.Error(err, "error configuring injection sidecar container", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection sidecar container: %s", err))
	}
	pod.Spec.Containers = append(pod.Spec.Containers, envoySidecar)

	// Now that the consul-sidecar no longer needs to re-register services periodically
	// (that functionality lives in the endpoints-controller),
	// we only need the consul sidecar to run the metrics merging server.
	// First, determine if we need to run the metrics merging server.
	shouldRunMetricsMerging, err := h.MetricsConfig.shouldRunMergedMetricsServer(pod)
	if err != nil {
		h.Log.Error(err, "error determining if metrics merging server should be run", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error determining if metrics merging server should be run: %s", err))
	}

	// Add the consul-sidecar only if we need to run the metrics merging server.
	if shouldRunMetricsMerging {
		consulSidecar, err := h.consulSidecar(pod)
		if err != nil {
			h.Log.Error(err, "error configuring consul sidecar container", "request name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring consul sidecar container: %s", err))
		}
		pod.Spec.Containers = append(pod.Spec.Containers, consulSidecar)
	}

	// pod.Annotations has already been initialized by h.defaultAnnotations()
	// and does not need to be checked for being a nil value.
	pod.Annotations[keyInjectStatus] = injected

	// Add annotations for metrics.
	if err = h.prometheusAnnotations(&pod); err != nil {
		h.Log.Error(err, "error configuring prometheus annotations", "request name", req.Name)
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
	if h.EnableNamespaces {
		pod.Annotations[annotationConsulNamespace] = h.consulNamespace(req.Namespace)
	}

	// Overwrite readiness/liveness probes if needed.
	err = h.overwriteProbes(*ns, &pod)
	if err != nil {
		h.Log.Error(err, "error overwriting readiness or liveness probes", "request name", req.Name)
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
	if h.EnableNamespaces {
		if _, err := namespaces.EnsureExists(h.ConsulClient, h.consulNamespace(req.Namespace), h.CrossNamespaceACLPolicy); err != nil {
			h.Log.Error(err, "error checking or creating namespace",
				"ns", h.consulNamespace(req.Namespace), "request name", req.Name)
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
func (h *Handler) overwriteProbes(ns corev1.Namespace, pod *corev1.Pod) error {
	tproxyEnabled, err := transparentProxyEnabled(ns, *pod, h.EnableTransparentProxy)
	if err != nil {
		return err
	}

	overwriteProbes, err := shouldOverwriteProbes(*pod, h.TProxyOverwriteProbes)
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

func (h *Handler) shouldInject(pod corev1.Pod, namespace string) (bool, error) {
	// Don't inject in the Kubernetes system namespaces
	if kubeSystemNamespaces.Contains(namespace) {
		return false, nil
	}

	// Namespace logic
	// If in deny list, don't inject
	if h.DenyK8sNamespacesSet.Contains(namespace) {
		return false, nil
	}

	// If not in allow list or allow list is not *, don't inject
	if !h.AllowK8sNamespacesSet.Contains("*") && !h.AllowK8sNamespacesSet.Contains(namespace) {
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

	return !h.RequireAnnotation, nil
}

func (h *Handler) defaultAnnotations(pod *corev1.Pod, podJson string) error {
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
func (h *Handler) prometheusAnnotations(pod *corev1.Pod) error {
	enableMetrics, err := h.MetricsConfig.enableMetrics(*pod)
	if err != nil {
		return err
	}
	prometheusScrapePort, err := h.MetricsConfig.prometheusScrapePort(*pod)
	if err != nil {
		return err
	}
	prometheusScrapePath := h.MetricsConfig.prometheusScrapePath(*pod)

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
func (h *Handler) consulNamespace(ns string) string {
	return namespaces.ConsulNamespace(ns, h.EnableNamespaces, h.ConsulDestinationNamespace, h.EnableK8SNSMirroring, h.K8SNSMirroringPrefix)
}

func (h *Handler) validatePod(pod corev1.Pod) error {
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

func findServiceAccountVolumeMount(pod corev1.Pod) (corev1.VolumeMount, error) {
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
		return volumeMount, errors.New("unable to find service account token volumeMount")
	}

	return volumeMount, nil
}

func (h *Handler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}
