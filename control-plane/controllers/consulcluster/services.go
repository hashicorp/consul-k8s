// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func (r *ConsulClusterReconciler) ensureHeadlessService(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	desired := buildHeadlessService(cluster)
	if err := r.setOwner(ctx, cluster, desired); err != nil {
		return err
	}
	return r.applyService(ctx, desired)
}

// applyService creates the Service or updates the fields the operator owns.
// ClusterIP and the allocated NodePorts are assigned by the API server and must
// be carried over from the live object, or the update is rejected.
func (r *ConsulClusterReconciler) applyService(ctx context.Context, desired *corev1.Service) error {
	existing := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if k8serrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Spec.Selector = desired.Spec.Selector
	existing.Spec.Type = desired.Spec.Type
	existing.Spec.PublishNotReadyAddresses = desired.Spec.PublishNotReadyAddresses
	existing.Spec.Ports = mergeServicePorts(existing.Spec.Ports, desired.Spec.Ports)
	return r.Patch(ctx, existing, patch)
}

// mergeServicePorts keeps the NodePort the API server allocated for a port
// unless the spec asks for a specific one.
func mergeServicePorts(existing, desired []corev1.ServicePort) []corev1.ServicePort {
	allocated := make(map[string]int32, len(existing))
	for _, p := range existing {
		allocated[p.Name] = p.NodePort
	}
	for i := range desired {
		if desired[i].NodePort == 0 {
			desired[i].NodePort = allocated[desired[i].Name]
		}
	}
	return desired
}

func (r *ConsulClusterReconciler) setOwner(_ context.Context, cluster *v1alpha1.ConsulCluster, obj client.Object) error {
	obj.SetOwnerReferences([]metav1.OwnerReference{ownerRef(cluster, r.Scheme)})
	return nil
}

func buildHeadlessService(cluster *v1alpha1.ConsulCluster) *corev1.Service {
	// PublishNotReadyAddresses covers this; the alpha annotation it replaced has
	// been ignored since Kubernetes 1.11.
	annotations := map[string]string{}
	for k, v := range cluster.Spec.ServiceAnnotations {
		annotations[k] = v
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        headlessServiceName(cluster),
			Namespace:   cluster.Namespace,
			Labels:      serviceLabels(cluster),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Selector:                 serverPodLabels(cluster),
			Ports:                    serverServicePorts(cluster),
		},
	}
}

func serviceLabels(cluster *v1alpha1.ConsulCluster) map[string]string {
	return map[string]string{
		labelApp:       labelAppValue,
		labelComponent: labelComponentValue,
		labelCluster:   cluster.Name,
	}
}

// apiServicePorts returns the HTTP and HTTPS ports the cluster actually serves.
// Publishing a port the agent has disabled leaves an endpoint that refuses every
// connection.
func apiServicePorts(cluster *v1alpha1.ConsulCluster) []corev1.ServicePort {
	var ports []corev1.ServicePort
	if !tlsEnabled(cluster) || !cluster.Spec.TLS.HTTPSOnly {
		ports = append(ports, corev1.ServicePort{
			Name: "http", Port: 8500, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP,
		})
	}
	if tlsEnabled(cluster) {
		ports = append(ports, corev1.ServicePort{
			Name: "https", Port: 8501, TargetPort: intstr.FromString("https"), Protocol: corev1.ProtocolTCP,
		})
	}
	return ports
}

func serverServicePorts(cluster *v1alpha1.ConsulCluster) []corev1.ServicePort {
	return append(apiServicePorts(cluster), []corev1.ServicePort{
		{Name: "grpc", Port: 8502, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP},
		{Name: "serflan-tcp", Port: 8301, TargetPort: intstr.FromString("serflan-tcp"), Protocol: corev1.ProtocolTCP},
		{Name: "serflan-udp", Port: 8301, TargetPort: intstr.FromString("serflan-udp"), Protocol: corev1.ProtocolUDP},
		{Name: "serfwan-tcp", Port: 8302, TargetPort: intstr.FromString("serfwan-tcp"), Protocol: corev1.ProtocolTCP},
		{Name: "serfwan-udp", Port: 8302, TargetPort: intstr.FromString("serfwan-udp"), Protocol: corev1.ProtocolUDP},
		{Name: "server", Port: 8300, TargetPort: intstr.FromString("server"), Protocol: corev1.ProtocolTCP},
		{Name: "dns-tcp", Port: 8600, TargetPort: intstr.FromString("dns-tcp"), Protocol: corev1.ProtocolTCP},
		{Name: "dns-udp", Port: 8600, TargetPort: intstr.FromString("dns-udp"), Protocol: corev1.ProtocolUDP},
	}...)
}
