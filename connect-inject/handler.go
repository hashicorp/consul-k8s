package connectinject

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mattbaird/jsonpatch"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const (
	DefaultConsulImage = "consul:1.7.1"
	DefaultEnvoyImage  = "envoyproxy/envoy-alpine:v1.13.0"
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
	// consul-k8s lifecycle-sidecar command. This flag controls how often the
	// service is synced (i.e. re-registered) with the local agent.
	annotationSyncPeriod = "consul.hashicorp.com/connect-sync-period"
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
	// This image is used for the lifecycle-sidecar container.
	ImageConsulK8S string

	// RequireAnnotation means that the annotation must be given to inject.
	// If this is false, injection is default.
	RequireAnnotation bool

	// AuthMethod is the name of the Kubernetes Auth Method to
	// use for identity with connectInjection if ACLs are enabled
	AuthMethod string

	// WriteServiceDefaults controls whether injection should write a
	// service-defaults config entry for each service.
	// Requires an additional `protocol` parameter.
	WriteServiceDefaults bool

	// DefaultProtocol is the default protocol to use for central config
	// registrations. It will be overridden by a specific annotation.
	DefaultProtocol string

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

	// Resources checks if cpu and memory resources for sidecar pods and
	// init containers should be set. If this is false, no resources
	// will be set.
	Resources bool
	// CPULimit sets cpu limit for pods
	CPULimit string
	// MemoryLimit sets memory limit for pods
	MemoryLimit string
	// CPURequest sets cpu requests for pods
	CPURequest string
	// MemoryRequest sets memory requests for pods
	MemoryRequest string


	// Log
	Log hclog.Logger
}

// Handle is the http.HandlerFunc implementation that actually handles the
// webhook request for admission control. This should be registered or
// served via an HTTP server.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	h.Log.Info("Request received", "Method", r.Method, "URL", r.URL)

	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		msg := fmt.Sprintf("Invalid content-type: %q", ct)
		http.Error(w, msg, http.StatusBadRequest)
		h.Log.Error("Error on request", "err", msg, "Code", http.StatusBadRequest)
		return
	}

	var body []byte
	if r.Body != nil {
		var err error
		if body, err = ioutil.ReadAll(r.Body); err != nil {
			msg := fmt.Sprintf("Error reading request body: %s", err)
			http.Error(w, msg, http.StatusBadRequest)
			h.Log.Error("Error on request", "err", msg, "Code", http.StatusBadRequest)
			return
		}
	}
	if len(body) == 0 {
		msg := "Empty request body"
		http.Error(w, msg, http.StatusBadRequest)
		h.Log.Error("Error on request", "err", msg, "Code", http.StatusBadRequest)
		return
	}

	var admReq v1beta1.AdmissionReview
	var admResp v1beta1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admReq); err != nil {
		h.Log.Error("Could not decode admission request", "err", err)
		admResp.Response = admissionError(err)
	} else {
		admResp.Response = h.Mutate(admReq.Request)
	}

	resp, err := json.Marshal(&admResp)
	if err != nil {
		msg := fmt.Sprintf("Error marshalling admission response: %s", err)
		http.Error(w, msg, http.StatusInternalServerError)
		h.Log.Error("Error on request", "err", msg, "Code", http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(resp); err != nil {
		h.Log.Error("Error writing response", "err", err)
	}
}

// Mutate takes an admission request and performs mutation if necessary,
// returning the final API response.
func (h *Handler) Mutate(req *v1beta1.AdmissionRequest) *v1beta1.AdmissionResponse {
	// Decode the pod from the request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		h.Log.Error("Could not unmarshal request to pod", "err", err)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Could not unmarshal request to pod: %s", err),
			},
		}
	}

	// Build the basic response
	resp := &v1beta1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	// Accumulate any patches here
	var patches []jsonpatch.JsonPatchOperation

	// Setup the default annotation values that are used for the container.
	// This MUST be done before shouldInject is called since k.
	if err := h.defaultAnnotations(&pod, &patches); err != nil {
		h.Log.Error("Error creating default annotations", "err", err, "Request Name", req.Name)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Error creating default annotations: %s", err),
			},
		}
	}

	// Check if we should inject, for example we don't inject in the
	// system namespaces.
	if shouldInject, err := h.shouldInject(&pod, req.Namespace); err != nil {
		h.Log.Error("Error checking if should inject", "err", err, "Request Name", req.Name)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Error checking if should inject: %s", err),
			},
		}
	} else if !shouldInject {
		return resp
	}

	// Add our volume that will be shared by the init container and
	// the sidecar for passing data in the pod.
	patches = append(patches, addVolume(
		pod.Spec.Volumes,
		[]corev1.Volume{h.containerVolume()},
		"/spec/volumes")...)

	// Add the upstream services as environment variables for easy
	// service discovery.
	for i, container := range pod.Spec.InitContainers {
		patches = append(patches, addEnvVar(
			container.Env,
			h.containerEnvVars(&pod),
			fmt.Sprintf("/spec/initContainers/%d/env", i))...)
	}
	for i, container := range pod.Spec.Containers {
		patches = append(patches, addEnvVar(
			container.Env,
			h.containerEnvVars(&pod),
			fmt.Sprintf("/spec/containers/%d/env", i))...)
	}

	// Add the init container that registers the service and sets up
	// the Envoy configuration.
	container, err := h.containerInit(&pod, req.Namespace)
	if err != nil {
		h.Log.Error("Error configuring injection init container", "err", err, "Request Name", req.Name)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Error configuring injection init container: %s", err),
			},
		}
	}
	patches = append(patches, addContainer(
		pod.Spec.InitContainers,
		[]corev1.Container{container},
		"/spec/initContainers")...)

	// Add the Envoy and lifecycle sidecars.
	esContainer, err := h.envoySidecar(&pod, req.Namespace)
	if err != nil {
		h.Log.Error("Error configuring injection sidecar container", "err", err, "Request Name", req.Name)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("Error configuring injection sidecar container: %s", err),
			},
		}
	}
	connectContainer := h.lifecycleSidecar(&pod)
	patches = append(patches, addContainer(
		pod.Spec.Containers,
		[]corev1.Container{esContainer, connectContainer},
		"/spec/containers")...)

	// Add annotations so that we know we're injected
	patches = append(patches, updateAnnotation(
		pod.Annotations,
		map[string]string{annotationStatus: "injected"})...)

	// Generate the patch
	var patch []byte
	if len(patches) > 0 {
		var err error
		patch, err = json.Marshal(patches)
		if err != nil {
			h.Log.Error("Could not marshal patches", "err", err, "Request Name", req.Name)
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: fmt.Sprintf("Could not marshal patches: %s", err),
				},
			}
		}

		resp.Patch = patch
		patchType := v1beta1.PatchTypeJSONPatch
		resp.PatchType = &patchType
	}

	// Check and potentially create Consul resources. This is done after
	// all patches are created to guarantee no errors were encountered in
	// that process before modifying the Consul cluster.
	if h.EnableNamespaces {
		// Check if the namespace exists. If not, create it.
		if err := h.checkAndCreateNamespace(h.consulNamespace(req.Namespace)); err != nil {
			h.Log.Error("Error checking or creating namespace", "err", err,
				"Namespace", h.consulNamespace(req.Namespace), "Request Name", req.Name)
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: fmt.Sprintf("Error checking or creating namespace: %s", err),
				},
			}
		}
	}

	return resp
}

func (h *Handler) shouldInject(pod *corev1.Pod, namespace string) (bool, error) {
	// Don't inject in the Kubernetes system namespaces
	if kubeSystemNamespaces.Contains(namespace) {
		return false, nil
	}

	// Namespace logic
	if h.EnableNamespaces {
		// If in deny list, don't inject
		if h.DenyK8sNamespacesSet.Contains(namespace) {
			return false, nil
		}

		// If not in allow list or allow list is not *, don't inject
		if !h.AllowK8sNamespacesSet.Contains("*") && !h.AllowK8sNamespacesSet.Contains(namespace) {
			return false, nil
		}
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

func (h *Handler) defaultAnnotations(pod *corev1.Pod, patches *[]jsonpatch.JsonPatchOperation) error {
	if pod.ObjectMeta.Annotations == nil {
		pod.ObjectMeta.Annotations = make(map[string]string)
	}

	// Default service name is the name of the first container.
	if _, ok := pod.ObjectMeta.Annotations[annotationService]; !ok {
		if cs := pod.Spec.Containers; len(cs) > 0 {
			// Create the patch for this first, so that the Annotation
			// object will be created if necessary
			*patches = append(*patches, updateAnnotation(
				pod.Annotations,
				map[string]string{annotationService: cs[0].Name})...)

			// Set the annotation for checking in shouldInject
			pod.ObjectMeta.Annotations[annotationService] = cs[0].Name
		}
	}

	// Default service port is the first port exported in the container
	if _, ok := pod.ObjectMeta.Annotations[annotationPort]; !ok {
		if cs := pod.Spec.Containers; len(cs) > 0 {
			if ps := cs[0].Ports; len(ps) > 0 {
				if ps[0].Name != "" {
					// Create the patch for this first, so that the Annotation
					// object will be created if necessary
					*patches = append(*patches, updateAnnotation(
						pod.Annotations,
						map[string]string{annotationPort: ps[0].Name})...)

					pod.ObjectMeta.Annotations[annotationPort] = ps[0].Name
				} else {
					// Create the patch for this first, so that the Annotation
					// object will be created if necessary
					*patches = append(*patches, updateAnnotation(
						pod.Annotations,
						map[string]string{annotationPort: strconv.Itoa(int(ps[0].ContainerPort))})...)

					pod.ObjectMeta.Annotations[annotationPort] = strconv.Itoa(int(ps[0].ContainerPort))
				}
			}
		}
	}

	if h.WriteServiceDefaults {
		// Default protocol is specified by a flag if not explicitly annotated
		if _, ok := pod.ObjectMeta.Annotations[annotationProtocol]; !ok && h.DefaultProtocol != "" {
			if cs := pod.Spec.Containers; len(cs) > 0 {
				// Create the patch for this first, so that the Annotation
				// object will be created if necessary
				*patches = append(*patches, updateAnnotation(
					pod.Annotations,
					map[string]string{annotationProtocol: h.DefaultProtocol})...)

				// Set the annotation for protocol
				pod.ObjectMeta.Annotations[annotationProtocol] = h.DefaultProtocol
			}
		}
	}

	return nil
}

// consulNamespace returns the namespace that a service should be
// registered in based on the namespace options. It returns an
// empty string if namespaces aren't enabled.
func (h *Handler) consulNamespace(ns string) string {
	if !h.EnableNamespaces {
		return ""
	}

	// Mirroring takes precedence
	if h.EnableK8SNSMirroring {
		return fmt.Sprintf("%s%s", h.K8SNSMirroringPrefix, ns)
	} else {
		return h.ConsulDestinationNamespace
	}
}

func (h *Handler) checkAndCreateNamespace(ns string) error {
	// Check if the Consul namespace exists
	namespaceInfo, _, err := h.ConsulClient.Namespaces().Read(ns, nil)
	if err != nil {
		return err
	}

	// If not, create it
	if namespaceInfo == nil {
		var aclConfig api.NamespaceACLConfig
		if h.CrossNamespaceACLPolicy != "" {
			// Create the ACLs config for the cross-Consul-namespace
			// default policy that needs to be attached
			aclConfig = api.NamespaceACLConfig{
				PolicyDefaults: []api.ACLLink{
					{Name: h.CrossNamespaceACLPolicy},
				},
			}
		}

		consulNamespace := api.Namespace{
			Name:        ns,
			Description: "Auto-generated by a Connect Injector",
			ACLs:        &aclConfig,
			Meta:        map[string]string{"external-source": "kubernetes"},
		}

		_, _, err = h.ConsulClient.Namespaces().Create(&consulNamespace, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func portValue(pod *corev1.Pod, value string) (int32, error) {
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

func admissionError(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

func findServiceAccountVolumeMount(pod *corev1.Pod) (corev1.VolumeMount, error) {
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
