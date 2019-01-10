package connectinject

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"

	"github.com/mattbaird/jsonpatch"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const (
	DefaultConsulImage = "consul:1.3.0"
	DefaultEnvoyImage  = "envoyproxy/envoy-alpine:v1.8.0"
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

	// annotationUpstreams is a list of upstreams to register with the
	// proxy in the format of `<service-name>:<local-port>,...`. The
	// service name should map to a Consul service namd and the local port
	// is the local port in the pod that the listener will bind to. It can
	// be a named port.
	annotationUpstreams = "consul.hashicorp.com/connect-service-upstreams"
)

var (
	codecs       = serializer.NewCodecFactory(runtime.NewScheme())
	deserializer = codecs.UniversalDeserializer()

	// kubeSystemNamespaces is a list of namespaces that are considered
	// "system" level namespaces and are always skipped (never injected).
	kubeSystemNamespaces = []string{
		metav1.NamespaceSystem,
		metav1.NamespacePublic,
	}
)

// Handler is the HTTP handler for admission webhooks.
type Handler struct {
	// ImageConsul is the container image for Consul to use.
	// ImageEnvoy is the container image for Envoy to use.
	//
	// Both of these MUST be set.
	ImageConsul string
	ImageEnvoy  string

	// RequireAnnotation means that the annotation must be given to inject.
	// If this is false, injection is default.
	RequireAnnotation bool
}

// Handle is the http.HandlerFunc implementation that actually handles the
// webhook request for admission control. This should be registered or
// served via an HTTP server.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		http.Error(w, fmt.Sprintf("Invalid content-type: %q", ct), http.StatusBadRequest)
		return
	}

	var body []byte
	if r.Body != nil {
		var err error
		if body, err = ioutil.ReadAll(r.Body); err != nil {
			http.Error(w, fmt.Sprintf(
				"Error reading request body: %s", err), http.StatusBadRequest)
			return
		}
	}
	if len(body) == 0 {
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	var admReq v1beta1.AdmissionReview
	var admResp v1beta1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admReq); err != nil {
		log.Printf("Could not decode admission request: %s", err)
		admResp.Response = admissionError(err)
	} else {
		admResp.Response = h.Mutate(admReq.Request)
	}

	resp, err := json.Marshal(&admResp)
	if err != nil {
		http.Error(w, fmt.Sprintf(
			"Error marshalling admission response: %s", err),
			http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(resp); err != nil {
		log.Printf("error writing response: %s", err)
	}
}

// Mutate takes an admission request and performs mutation if necessary,
// returning the final API response.
func (h *Handler) Mutate(req *v1beta1.AdmissionRequest) *v1beta1.AdmissionResponse {
	// Decode the pod from the request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		log.Printf("Could not unmarshal request to pod: %s", err)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
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
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	// Check if we should inject, for example we don't inject in the
	// system namespaces.
	if shouldInject, err := h.shouldInject(&pod); err != nil {
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
	container, err := h.containerInit(&pod)
	if err != nil {
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

	// Add the Envoy sidecar
	patches = append(patches, addContainer(
		pod.Spec.Containers,
		[]corev1.Container{h.containerSidecar(&pod)},
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
			log.Printf("Could not marshal patches: %s", err)
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}

		resp.Patch = patch
		patchType := v1beta1.PatchTypeJSONPatch
		resp.PatchType = &patchType
	}

	return resp
}

func (h *Handler) shouldInject(pod *corev1.Pod) (bool, error) {
	// Don't inject in the Kubernetes system namespaces
	for _, ns := range kubeSystemNamespaces {
		if pod.ObjectMeta.Namespace == ns {
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
