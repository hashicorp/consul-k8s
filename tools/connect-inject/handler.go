package connectinject

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/mattbaird/jsonpatch"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const (
	// annotationStatus is the key of the annotation that is added to
	// a pod after an injection is done.
	annotationStatus = "consul.hashicorp.com/connect-inject-status"
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

	// Check if we should inject, for example we don't inject in the
	// system namespaces.
	if !h.shouldInject(&pod) {
		return resp
	}

	// Accumulate any patches here
	var patches []jsonpatch.JsonPatchOperation

	// Add a container to it
	patches = append(patches, addContainer(
		pod.Spec.Containers,
		[]corev1.Container{h.containerSidecar()},
		"/spec/containers")...)
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

func (h *Handler) shouldInject(pod *corev1.Pod) bool {
	// Don't inject in the Kubernetes system namespaces
	for _, ns := range kubeSystemNamespaces {
		if pod.ObjectMeta.Namespace == ns {
			return false
		}
	}

	return true
}

func (h *Handler) containerSidecar() corev1.Container {
	return corev1.Container{
		Name:  "consul-connect-proxy",
		Image: "consul:1.2.2",
		Env: []corev1.EnvVar{
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
		},
		Command: []string{
			"/bin/sh", "-ec",
		},
	}
}

func admissionError(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

func addContainer(target, add []corev1.Container, base string) []jsonpatch.JsonPatchOperation {
	var result []jsonpatch.JsonPatchOperation
	first := len(target) == 0
	var value interface{}
	for _, container := range add {
		value = container
		path := base
		if first {
			first = false
			value = []corev1.Container{container}
		} else {
			path = path + "/-"
		}

		result = append(result, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      path,
			Value:     value,
		})
	}

	return result
}

func updateAnnotation(target, add map[string]string) []jsonpatch.JsonPatchOperation {
	var result []jsonpatch.JsonPatchOperation
	for key, value := range add {
		if target == nil || target[key] == "" {
			target = map[string]string{}
			result = append(result, jsonpatch.JsonPatchOperation{
				Operation: "add",
				Path:      "/metadata/annotations",
				Value: map[string]string{
					key: value,
				},
			})
		} else {
			result = append(result, jsonpatch.JsonPatchOperation{
				Operation: "replace",
				Path:      "/metadata/annotations/" + key,
				Value:     value,
			})
		}
	}
	return result
}
