// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

// ambientRedirectConfig is the JSON stored in constants.AnnotationAmbientRedirectConfig and consumed
// by the CNI plugin. Field names must match the CNI plugin's ambientRedirectConfig.
type ambientRedirectConfig struct {
	InboundPort          uint32
	OutboundPort         uint32
	ProxyMark            uint32
	ConsulDNSIP          string
	ConsulDNSPort        int
	ProbePorts           []string
	ExcludeInboundPorts  []string
	ExcludeOutboundPorts []string
	ExcludeOutboundCIDRs []string
	DualStack            bool
}

// ambientMeshRequested returns true if the pod opts into the ambient mesh, either via its own
// annotation or via its namespace label.
func (w *MeshWebhook) ambientMeshRequested(ns corev1.Namespace, pod corev1.Pod) (bool, error) {
	return common.AmbientMeshEnabled(ns, pod, w.EnableAmbientMesh)
}

// ambientShouldInject mirrors shouldInject for ambient pods. Opting into the ambient mesh implies
// injection, so RequireAnnotation does not apply, but an explicit connect-inject=false still wins.
func (w *MeshWebhook) ambientShouldInject(pod corev1.Pod, namespace string) (bool, error) {
	if kubeSystemNamespaces.Contains(namespace) || w.DenyK8sNamespacesSet.Contains(namespace) {
		return false, nil
	}
	if !w.AllowK8sNamespacesSet.Contains("*") && !w.AllowK8sNamespacesSet.Contains(namespace) {
		return false, nil
	}
	if pod.Annotations[constants.KeyInjectStatus] != "" {
		return false, nil
	}
	if raw, ok := pod.Annotations[constants.AnnotationInject]; ok {
		return strconv.ParseBool(raw)
	}
	return true, nil
}

// handleAmbient mutates a pod enrolled in the ambient mesh. No init container or sidecar is added:
// the CNI plugin redirects the pod's traffic to the node-level proxy and the endpoints controller
// registers the pod's proxy with Consul.
func (w *MeshWebhook) handleAmbient(ctx context.Context, req admission.Request, ns corev1.Namespace, pod corev1.Pod, origPodJson []byte) admission.Response {
	// Only the CNI plugin may set the network namespace and only the webhook may set the status.
	delete(pod.Annotations, constants.AnnotationAmbientNetNS)
	delete(pod.Annotations, constants.KeyAmbientMeshStatus)

	if shouldInject, err := w.ambientShouldInject(pod, req.Namespace); err != nil {
		w.Log.Error(err, "error checking if should inject ambient pod", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking if should inject: %s", err))
	} else if !shouldInject {
		return admission.Allowed(fmt.Sprintf("%s %s does not require injection", pod.Kind, pod.Name))
	}

	if err := w.validateAmbientPod(pod); err != nil {
		w.Log.Error(err, "unsupported ambient mesh pod", "request name", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	w.Log.Info("received ambient mesh pod", "name", req.Name, "ns", req.Namespace)

	// Upstream env vars are kept so that explicit upstreams behave the same as with a sidecar.
	containerEnvVars := w.containerEnvVars(pod)
	for i := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[i].Env = append(pod.Spec.InitContainers[i].Env, containerEnvVars...)
	}
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, containerEnvVars...)
	}

	dnsEnabled, err := ambientConsulDNSEnabled(ns, pod, w.EnableConsulDNS)
	if err != nil {
		w.Log.Error(err, "error determining if dns redirection is enabled", "request name", req.Name)
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("error determining if dns redirection is enabled: %s", err))
	}
	if dnsEnabled {
		if err = w.configureDNS(&pod, req.Namespace); err != nil {
			w.Log.Error(err, "error configuring DNS on the pod", "request name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error configuring DNS on the pod: %s", err))
		}
	}

	redirectCfg, err := w.ambientRedirectConfigJSON(pod, dnsEnabled)
	if err != nil {
		w.Log.Error(err, "error building ambient redirect config", "request name", req.Name)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error building ambient redirect config: %s", err))
	}

	pod.Annotations[constants.KeyInjectStatus] = constants.Injected
	pod.Annotations[constants.KeyAmbientMeshStatus] = constants.AmbientMeshStatusWaiting
	pod.Annotations[constants.AnnotationAmbientRedirectConfig] = redirectCfg
	if err := w.addDualStackAnnotation(&pod); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("unable to set dual stack annotation to pod with error: %s", err))
	}
	if w.EnableNamespaces {
		pod.Annotations[constants.AnnotationConsulNamespace] = w.consulNamespace(req.Namespace)
	}

	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[constants.KeyInjectStatus] = constants.Injected
	pod.Labels[constants.KeyManagedBy] = constants.ManagedByValue
	pod.Labels[constants.KeyAmbientMesh] = constants.LabelAmbientMeshEnabled

	updatedPodJson, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	patches, err := jsonpatch.CreatePatch(origPodJson, updatedPodJson)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if w.EnableNamespaces {
		if err := w.ensureAmbientConsulNamespace(req.Namespace); err != nil {
			w.Log.Error(err, "error checking or creating namespace",
				"ns", w.consulNamespace(req.Namespace), "request name", req.Name)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking or creating namespace: %s", err))
		}
	}

	return admission.Patched(fmt.Sprintf("valid ambient %s request", pod.Kind), patches...)
}

// validateAmbientPod rejects configurations the node-level proxy does not support in this POC.
func (w *MeshWebhook) validateAmbientPod(pod corev1.Pod) error {
	if len(w.annotatedServiceNames(pod)) > 1 {
		return fmt.Errorf("multi-port pods are not supported in the ambient mesh")
	}
	if _, ok := pod.Annotations[constants.AnnotationAIRole]; ok {
		return fmt.Errorf("%s pods are not supported in the ambient mesh", constants.AnnotationAIRole)
	}
	return nil
}

// ambientConsulDNSEnabled is consulDNSEnabled without the transparent proxy requirement: ambient
// pods always have their traffic redirected by the CNI plugin.
func ambientConsulDNSEnabled(namespace corev1.Namespace, pod corev1.Pod, globalDNSEnabled bool) (bool, error) {
	if raw, ok := pod.Annotations[constants.KeyConsulDNS]; ok {
		return strconv.ParseBool(raw)
	}
	if raw, ok := namespace.Labels[constants.KeyConsulDNS]; ok {
		return strconv.ParseBool(raw)
	}
	return globalDNSEnabled, nil
}

func (w *MeshWebhook) ambientRedirectConfigJSON(pod corev1.Pod, dnsEnabled bool) (string, error) {
	probePorts, err := ambientProbePorts(pod)
	if err != nil {
		return "", err
	}
	cfg := ambientRedirectConfig{
		InboundPort:          uint32(w.AmbientInboundPort),
		OutboundPort:         uint32(w.AmbientOutboundPort),
		ProxyMark:            uint32(w.AmbientProxyMark),
		ProbePorts:           probePorts,
		ExcludeInboundPorts:  splitCommaSeparatedItemsFromAnnotation(constants.AnnotationTProxyExcludeInboundPorts, pod),
		ExcludeOutboundPorts: splitCommaSeparatedItemsFromAnnotation(constants.AnnotationTProxyExcludeOutboundPorts, pod),
		ExcludeOutboundCIDRs: splitCommaSeparatedItemsFromAnnotation(constants.AnnotationTProxyExcludeOutboundCIDRs, pod),
		DualStack:            constants.IsDualStack(),
	}
	if dnsEnabled {
		// The node proxy serves Consul DNS inside the pod network namespace on this address.
		cfg.ConsulDNSIP = constants.Getv4orv6Str(consulDataplaneDNSBindHost, ipv6ConsulDataplaneDNSBindHost)
		cfg.ConsulDNSPort = consulDataplaneDNSBindPort
	}
	out, err := json.Marshal(&cfg)
	if err != nil {
		return "", fmt.Errorf("could not marshal ambient redirect config: %w", err)
	}
	return string(out), nil
}

// ambientProbePorts returns the ports used by kubelet HTTP/TCP/gRPC probes. Kubelet probes come
// from the node IP without mTLS, so the CNI plugin exempts them from inbound redirection.
func ambientProbePorts(pod corev1.Pod) ([]string, error) {
	set := map[int32]struct{}{}
	add := func(p intstr.IntOrString) error {
		port, err := common.PortValue(pod, p.String())
		if err != nil {
			return fmt.Errorf("invalid probe port %q: %w", p.String(), err)
		}
		if port > 0 {
			set[port] = struct{}{}
		}
		return nil
	}
	for _, c := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
		for _, probe := range []*corev1.Probe{c.LivenessProbe, c.ReadinessProbe, c.StartupProbe} {
			if probe == nil {
				continue
			}
			switch {
			case probe.HTTPGet != nil:
				if err := add(probe.HTTPGet.Port); err != nil {
					return nil, err
				}
			case probe.TCPSocket != nil:
				if err := add(probe.TCPSocket.Port); err != nil {
					return nil, err
				}
			case probe.GRPC != nil:
				set[probe.GRPC.Port] = struct{}{}
			}
		}
	}
	ports := make([]string, 0, len(set))
	for p := range set {
		ports = append(ports, strconv.Itoa(int(p)))
	}
	sort.Strings(ports)
	return ports, nil
}

func (w *MeshWebhook) ensureAmbientConsulNamespace(k8sNS string) error {
	serverState, err := w.ConsulServerConnMgr.State()
	if err != nil {
		return err
	}
	apiClient, err := consul.NewClientFromConnMgrState(w.ConsulConfig, serverState)
	if err != nil {
		return err
	}
	_, err = namespaces.EnsureExists(apiClient, w.consulNamespace(k8sNS), w.CrossNamespaceACLPolicy)
	return err
}
