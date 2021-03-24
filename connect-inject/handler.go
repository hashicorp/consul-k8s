package connectinject

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/namespaces"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// annotationStatus is the key of the annotation that is added to
	// a pod after an injection is done.
	annotationStatus = "consul.hashicorp.com/connect-inject-status"

	// annotationInject is the key of the annotation that controls whether
	// injection is explicitly enabled or disabled for a pod. This should
	// be set to a truthy or falsy value, as parseable by strconv.ParseBool
	annotationInject = "consul.hashicorp.com/connect-inject"

	// annotationService is the name of the service to proxy. This defaults
	// to the name of the first container.
	annotationService = "consul.hashicorp.com/connect-service"

	// annotationPort is the name or value of the port to proxy incoming
	// connections to.
	annotationPort = "consul.hashicorp.com/connect-service-port"

	// annotationProtocol contains the protocol that should be used for
	// the service that is being injected. Valid values are "http", "http2",
	// "grpc" and "tcp".
	//
	// Deprecated: This annotation is no longer supported.
	annotationProtocol = "consul.hashicorp.com/connect-service-protocol"

	// annotationUpstreams is a list of upstreams to register with the
	// proxy in the format of `<service-name>:<local-port>,...`. The
	// service name should map to a Consul service namd and the local port
	// is the local port in the pod that the listener will bind to. It can
	// be a named port.
	annotationUpstreams = "consul.hashicorp.com/connect-service-upstreams"

	// annotationTags is a list of tags to register with the service
	// this is specified as a comma separated list e.g. abc,123
	annotationTags = "consul.hashicorp.com/service-tags"

	// annotationConnectTags is a list of tags to register with the service
	// this is specified as a comma separated list e.g. abc,123
	//
	// Deprecated: 'consul.hashicorp.com/service-tags' is the new annotation
	// and should be used instead. We made this change because the tagging is
	// not specific to connect as both the connect proxy *and* the Consul
	// service that gets registered is tagged.
	annotationConnectTags = "consul.hashicorp.com/connect-service-tags"

	// annotationMeta is a list of metadata key/value pairs to add to the service
	// registration. This is specified in the format `<key>:<value>`
	// e.g. consul.hashicorp.com/service-meta-foo:bar
	annotationMeta = "consul.hashicorp.com/service-meta-"

	// annotationSyncPeriod controls the -sync-period flag passed to the
	// consul-k8s consul-sidecar command. This flag controls how often the
	// service is synced (i.e. re-registered) with the local agent.
	annotationSyncPeriod = "consul.hashicorp.com/connect-sync-period"

	// annotations for sidecar proxy resource limits
	annotationSidecarProxyCPULimit      = "consul.hashicorp.com/sidecar-proxy-cpu-limit"
	annotationSidecarProxyCPURequest    = "consul.hashicorp.com/sidecar-proxy-cpu-request"
	annotationSidecarProxyMemoryLimit   = "consul.hashicorp.com/sidecar-proxy-memory-limit"
	annotationSidecarProxyMemoryRequest = "consul.hashicorp.com/sidecar-proxy-memory-request"

	// annotations for metrics to configure where Prometheus scrapes
	// metrics from, whether to run a merged metrics endpoint on the consul
	// sidecar, and configure the connect service metrics.
	annotationEnableMetrics        = "consul.hashicorp.com/enable-metrics"
	annotationEnableMetricsMerging = "consul.hashicorp.com/enable-metrics-merging"
	annotationMergedMetricsPort    = "consul.hashicorp.com/merged-metrics-port"
	annotationPrometheusScrapePort = "consul.hashicorp.com/prometheus-scrape-port"
	annotationPrometheusScrapePath = "consul.hashicorp.com/prometheus-scrape-path"
	annotationServiceMetricsPort   = "consul.hashicorp.com/service-metrics-port"
	annotationServiceMetricsPath   = "consul.hashicorp.com/service-metrics-path"

	// annotationEnvoyExtraArgs is a space-separated list of arguments to be passed to the
	// envoy binary. See list of args here: https://www.envoyproxy.io/docs/envoy/latest/operations/cli
	// e.g. consul.hashicorp.com/envoy-extra-args: "--log-level debug --disable-hot-restart"
	// The arguments passed in via this annotation will take precendence over arguments
	// passed via the -envoy-extra-args flag.
	annotationEnvoyExtraArgs = "consul.hashicorp.com/envoy-extra-args"

	// injected is used as the annotation value for annotationInjected
	injected = "injected"

	// annotationConsulNamespace is the Consul namespace the service is registered into.
	annotationConsulNamespace = "consul.hashicorp.com/consul-namespace"

	defaultServiceMetricsPath = "/metrics"
)

var (
	codecs       = serializer.NewCodecFactory(runtime.NewScheme())
	deserializer = codecs.UniversalDeserializer()

	// kubeSystemNamespaces is a set of namespaces that are considered
	// "system" level namespaces and are always skipped (never injected).
	kubeSystemNamespaces = mapset.NewSetWith(metav1.NamespaceSystem, metav1.NamespacePublic)
)

// Handler is the HTTP handler for admission webhooks.
type Handler struct {
	ConsulClient *api.Client

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
	// use for identity with connectInjection if ACLs are enabled
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

	// Default metrics settings. These will configure where Prometheus scrapes
	// metrics from, and whether to run a merged metrics endpoint on the consul
	// sidecar. These can be overridden via pod annotations.
	DefaultEnableMetrics        bool
	DefaultEnableMetricsMerging bool
	DefaultMergedMetricsPort    string
	DefaultPrometheusScrapePort string
	DefaultPrometheusScrapePath string

	// Resource settings for init container. All of these fields
	// will be populated by the defaults provided in the initial flags.
	InitContainerResources corev1.ResourceRequirements

	// Resource settings for Consul sidecar. All of these fields
	// will be populated by the defaults provided in the initial flags.
	ConsulSidecarResources corev1.ResourceRequirements

	// Log
	Log hclog.Logger

	decoder *admission.Decoder
}

// Handle is the admission.Handler implementation that actually handles the
// webhook request for admission control. This should be registered or
// served via the controller runtime manager.
func (h *Handler) Handle(_ context.Context, req admission.Request) admission.Response {
	var pod corev1.Pod

	// Decode the pod from the request
	if err := h.decoder.Decode(req, &pod); err != nil {
		h.Log.Error("Could not unmarshal request to pod", "err", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Marshall the contents of the pod that was received. This is compared with the
	// marshalled contents of the pod after it has been updated to create the jsonpatch.
	origPodJson, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := h.validatePod(pod); err != nil {
		h.Log.Error("Error validating pod", "err", err, "Request Name", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Setup the default annotation values that are used for the container.
	// This MUST be done before shouldInject is called since that function
	// uses these annotations.
	if err := h.defaultAnnotations(&pod); err != nil {
		h.Log.Error("Error creating default annotations", "err", err, "Request Name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error creating default annotations: %s", err))
	}

	// Check if we should inject, for example we don't inject in the
	// system namespaces.
	if shouldInject, err := h.shouldInject(pod, req.Namespace); err != nil {
		h.Log.Error("Error checking if should inject", "err", err, "Request Name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking if should inject: %s", err))
	} else if !shouldInject {
		return admission.Allowed(fmt.Sprintf("%s %s does not require injection", pod.Kind, pod.Name))
	}

	// Add our volume that will be shared by the init container and
	// the sidecar for passing data in the pod.
	pod.Spec.Volumes = append(pod.Spec.Volumes, h.containerVolume())

	// Add the upstream services as environment variables for easy
	// service discovery.
	containerEnvVars := h.containerEnvVars(pod)
	for _, container := range pod.Spec.InitContainers {
		container.Env = append(container.Env, containerEnvVars...)
	}

	for _, container := range pod.Spec.Containers {
		container.Env = append(container.Env, containerEnvVars...)
	}

	// TODO: rename both of these initcontainers appropriately
	// Add the consul-init container
	initCopyContainer := h.containerInitCopyContainer()
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initCopyContainer)

	// Add the init container that registers the service and sets up
	// the Envoy configuration.
	initContainer, err := h.containerInit(pod, req.Namespace)
	if err != nil {
		h.Log.Error("Error configuring injection init container", "err", err, "Request Name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection init container: %s", err))
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

	// Add the Envoy and Consul sidecars.
	esContainer, err := h.envoySidecar(pod, req.Namespace)
	if err != nil {
		h.Log.Error("Error configuring injection sidecar container", "err", err, "Request Name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring injection sidecar container: %s", err))
	}
	pod.Spec.Containers = append(pod.Spec.Containers, esContainer)

	connectContainer, err := h.consulSidecar(pod)
	if err != nil {
		h.Log.Error("Error configuring consul sidecar container", "err", err, "Request Name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring consul sidecar container: %s", err))
	}
	pod.Spec.Containers = append(pod.Spec.Containers, connectContainer)

	// pod.Annotations has already been initialized by h.defaultAnnotations()
	// and does not need to be checked for being a nil value.
	pod.Annotations[annotationStatus] = injected

	// Add annotations for metrics.
	err = h.prometheusAnnotations(&pod)
	if err != nil {
		h.Log.Error("Error configuring prometheus annotations", "err", err, "Request Name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring prometheus annotations: %s", err))
	}

	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[labelInject] = injected

	// Consul-ENT only: Add the Consul destination namespace as an annotation to the pod.
	if h.EnableNamespaces {
		pod.Annotations[annotationConsulNamespace] = h.consulNamespace(req.Namespace)
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
			h.Log.Error("Error checking or creating namespace", "err", err,
				"Namespace", h.consulNamespace(req.Namespace), "Request Name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking or creating namespace: %s", err))
		}
	}

	// Return a Patched response along with the patches we intend on applying to the
	// Pod received by the handler.
	return admission.Patched(fmt.Sprintf("valid %s request", pod.Kind), patches...)
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
	if raw, ok := pod.Annotations[annotationInject]; ok {
		return strconv.ParseBool(raw)
	}

	return !h.RequireAnnotation, nil
}

func (h *Handler) defaultAnnotations(pod *corev1.Pod) error {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	// Default service name is the name of the first container.
	if _, ok := pod.ObjectMeta.Annotations[annotationService]; !ok {
		if cs := pod.Spec.Containers; len(cs) > 0 {
			// Set the annotation for checking in shouldInject
			pod.Annotations[annotationService] = cs[0].Name
		}
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

	return nil
}

// enableMetrics returns the default value in the handler, or overrides that
// with the annotation if provided.
func (h *Handler) enableMetrics(pod corev1.Pod) (bool, error) {
	enabled := h.DefaultEnableMetrics
	if raw, ok := pod.Annotations[annotationEnableMetrics]; ok && raw != "" {
		enableMetrics, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", annotationEnableMetrics, raw, err)
		}
		enabled = enableMetrics
	}
	return enabled, nil
}

// enableMetricsMerging returns the default value in the handler, or overrides
// that with the annotation if provided.
func (h *Handler) enableMetricsMerging(pod corev1.Pod) (bool, error) {
	enabled := h.DefaultEnableMetricsMerging
	if raw, ok := pod.Annotations[annotationEnableMetricsMerging]; ok && raw != "" {
		enableMetricsMerging, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", annotationEnableMetricsMerging, raw, err)
		}
		enabled = enableMetricsMerging
	}
	return enabled, nil
}

// mergedMetricsPort returns the default value in the handler, or overrides
// that with the annotation if provided.
func (h *Handler) mergedMetricsPort(pod corev1.Pod) (string, error) {
	return determineAndValidatePort(pod, annotationMergedMetricsPort, h.DefaultMergedMetricsPort, false)
}

// prometheusScrapePort returns the default value in the handler, or overrides
// that with the annotation if provided.
func (h *Handler) prometheusScrapePort(pod corev1.Pod) (string, error) {
	return determineAndValidatePort(pod, annotationPrometheusScrapePort, h.DefaultPrometheusScrapePort, false)
}

// prometheusScrapePath returns the default value in the handler, or overrides
// that with the annotation if provided.
func (h *Handler) prometheusScrapePath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[annotationPrometheusScrapePath]; ok && raw != "" {
		return raw
	}

	return h.DefaultPrometheusScrapePath
}

// serviceMetricsPort returns the port the service exposes metrics on. This will
// default to the port used to register the service with Consul, and can be
// overridden with the annotation if provided.
func (h *Handler) serviceMetricsPort(pod corev1.Pod) (string, error) {
	// The annotationPort is the port used to register the service with Consul.
	// If that has been set, it'll be used as the port for getting service
	// metrics as well, unless overridden by the service-metrics-port annotation.
	if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
		// The service metrics port can be privileged if the service author has
		// written their service in such a way that it expects to be able to use
		// privileged ports. So, the port metrics are exposed on the service can
		// be privileged.
		return determineAndValidatePort(pod, annotationServiceMetricsPort, raw, true)
	}

	// If the annotationPort is not set, the serviceMetrics port will be 0
	// unless overridden by the service-metrics-port annotation. If the service
	// metrics port is 0, the consul sidecar will not run a merged metrics
	// server.
	return determineAndValidatePort(pod, annotationServiceMetricsPort, "0", true)
}

// serviceMetricsPath returns a default of /metrics, or overrides
// that with the annotation if provided.
func (h *Handler) serviceMetricsPath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[annotationServiceMetricsPath]; ok && raw != "" {
		return raw
	}

	return defaultServiceMetricsPath
}

// prometheusAnnotations returns the Prometheus scraping configuration
// annotations. It returns a nil map if metrics are not enabled and annotations
// should not be set.
func (h *Handler) prometheusAnnotations(pod *corev1.Pod) error {
	enableMetrics, err := h.enableMetrics(*pod)
	if err != nil {
		return err
	}
	prometheusScrapePort, err := h.prometheusScrapePort(*pod)
	if err != nil {
		return err
	}
	prometheusScrapePath := h.prometheusScrapePath(*pod)

	if enableMetrics {
		pod.Annotations["prometheus.io/scrape"] = "true"
		pod.Annotations["prometheus.io/port"] = prometheusScrapePort
		pod.Annotations["prometheus.io/path"] = prometheusScrapePath
	}
	return nil
}

// shouldRunMergedMetricsServer returns whether we need to run a merged metrics
// server. This is used to configure the consul sidecar command, and the init
// container, so it can pass appropriate arguments to the consul connect envoy
// command.
func (h *Handler) shouldRunMergedMetricsServer(pod corev1.Pod) (bool, error) {
	enableMetrics, err := h.enableMetrics(pod)
	if err != nil {
		return false, err
	}
	enableMetricsMerging, err := h.enableMetricsMerging(pod)
	if err != nil {
		return false, err
	}
	serviceMetricsPort, err := h.serviceMetricsPort(pod)
	if err != nil {
		return false, err
	}

	// Don't need to check error here since serviceMetricsPort has been
	// validated by calling h.serviceMetricsPort above
	smp, _ := strconv.Atoi(serviceMetricsPort)

	if enableMetrics && enableMetricsMerging && smp > 0 {
		return true, nil
	}
	return false, nil
}

// determineAndValidatePort behaves as follows:
// If the annotation exists, validate the port and return it.
// If the annotation does not exist, return the default port.
// If the privileged flag is true, it will allow the port to be in the
// privileged port range of 1-1023. Otherwise, it will only allow ports in the
// unprivileged range of 1024-65535.
func determineAndValidatePort(pod corev1.Pod, annotation string, defaultPort string, privileged bool) (string, error) {
	if raw, ok := pod.Annotations[annotation]; ok && raw != "" {
		port, err := portValue(pod, raw)
		if err != nil {
			return "", fmt.Errorf("%s annotation value of %s is not a valid integer", annotation, raw)
		}

		if privileged && (port < 1 || port > 65535) {
			return "", fmt.Errorf("%s annotation value of %d is not in the valid port range 1-65535", annotation, port)
		} else if !privileged && (port < 1024 || port > 65535) {
			return "", fmt.Errorf("%s annotation value of %d is not in the unprivileged port range 1024-65535", annotation, port)
		}

		// if the annotation exists, return the validated port
		return fmt.Sprint(port), nil
	}

	// if the annotation does not exist, return the default
	if defaultPort != "" {
		port, err := portValue(pod, defaultPort)
		if err != nil {
			return "", fmt.Errorf("%s is not a valid port on the pod %s", defaultPort, pod.Name)
		}
		return fmt.Sprint(port), nil
	}
	return "", nil
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
	return nil
}

func portValue(pod corev1.Pod, value string) (int32, error) {
	// First search for the named port
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			if p.Name == value {
				return p.ContainerPort, nil
			}
		}
	}

	// Named port not found, return the parsed value
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
		return volumeMount, errors.New("Unable to find service account token volumeMount")
	}

	return volumeMount, nil
}

func (h *Handler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}
