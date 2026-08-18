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
	svc := &corev1.Service{}
	name := headlessServiceName(cluster)
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, svc)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return err
	}

	desired := buildHeadlessService(cluster)
	if setErr := r.setOwner(ctx, cluster, desired); setErr != nil {
		return setErr
	}
	return r.Create(ctx, desired)
}

func (r *ConsulClusterReconciler) ensureClientService(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	svc := &corev1.Service{}
	name := clientServiceName(cluster)
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, svc)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return err
	}

	desired := buildClientService(cluster)
	if setErr := r.setOwner(ctx, cluster, desired); setErr != nil {
		return setErr
	}
	return r.Create(ctx, desired)
}

func (r *ConsulClusterReconciler) ensureExposeService(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	if cluster.Spec.ExposeService == nil || !cluster.Spec.ExposeService.Enabled {
		return nil
	}

	svc := &corev1.Service{}
	name := exposeServiceName(cluster)
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, svc)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return err
	}

	desired := buildExposeService(cluster)
	if setErr := r.setOwner(ctx, cluster, desired); setErr != nil {
		return setErr
	}
	return r.Create(ctx, desired)
}

func (r *ConsulClusterReconciler) setOwner(_ context.Context, cluster *v1alpha1.ConsulCluster, obj client.Object) error {
	obj.SetOwnerReferences([]metav1.OwnerReference{ownerRef(cluster, r.Scheme)})
	return nil
}

func buildHeadlessService(cluster *v1alpha1.ConsulCluster) *corev1.Service {
	annotations := map[string]string{
		"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
	}
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
			Ports:                    serverServicePorts(),
		},
	}
}

func buildClientService(cluster *v1alpha1.ConsulCluster) *corev1.Service {
	annotations := map[string]string{}
	for k, v := range cluster.Spec.ServiceAnnotations {
		annotations[k] = v
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        clientServiceName(cluster),
			Namespace:   cluster.Namespace,
			Labels:      serviceLabels(cluster),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: serverPodLabels(cluster),
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8500, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
				{Name: "https", Port: 8501, TargetPort: intstr.FromString("https"), Protocol: corev1.ProtocolTCP},
				{Name: "grpc", Port: 8502, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP},
				{Name: "dns-tcp", Port: 8600, TargetPort: intstr.FromString("dns-tcp"), Protocol: corev1.ProtocolTCP},
				{Name: "dns-udp", Port: 8600, TargetPort: intstr.FromString("dns-udp"), Protocol: corev1.ProtocolUDP},
			},
		},
	}
}

func buildExposeService(cluster *v1alpha1.ConsulCluster) *corev1.Service {
	es := cluster.Spec.ExposeService

	svcType := corev1.ServiceTypeLoadBalancer
	if es.Type == "NodePort" {
		svcType = corev1.ServiceTypeNodePort
	}

	annotations := map[string]string{}
	for k, v := range es.Annotations {
		annotations[k] = v
	}

	ports := []corev1.ServicePort{
		{Name: "http", Port: 8500, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
		{Name: "https", Port: 8501, TargetPort: intstr.FromString("https"), Protocol: corev1.ProtocolTCP},
		{Name: "grpc", Port: 8502, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP},
		{Name: "serf", Port: 8301, TargetPort: intstr.FromString("serflan-tcp"), Protocol: corev1.ProtocolTCP},
		{Name: "rpc", Port: 8300, TargetPort: intstr.FromString("server"), Protocol: corev1.ProtocolTCP},
	}

	if es.NodePort != nil && svcType == corev1.ServiceTypeNodePort {
		np := es.NodePort
		nodePorts := []int32{np.HTTP, np.HTTPS, np.GRPC, np.Serf, np.RPC}
		for i, v := range nodePorts {
			if v != 0 {
				ports[i].NodePort = v
			}
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        exposeServiceName(cluster),
			Namespace:   cluster.Namespace,
			Labels:      serviceLabels(cluster),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: serverPodLabels(cluster),
			Ports:    ports,
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

func serverServicePorts() []corev1.ServicePort {
	return []corev1.ServicePort{
		{Name: "http", Port: 8500, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
		{Name: "https", Port: 8501, TargetPort: intstr.FromString("https"), Protocol: corev1.ProtocolTCP},
		{Name: "grpc", Port: 8502, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP},
		{Name: "serflan-tcp", Port: 8301, TargetPort: intstr.FromString("serflan-tcp"), Protocol: corev1.ProtocolTCP},
		{Name: "serflan-udp", Port: 8301, TargetPort: intstr.FromString("serflan-udp"), Protocol: corev1.ProtocolUDP},
		{Name: "serfwan-tcp", Port: 8302, TargetPort: intstr.FromString("serfwan-tcp"), Protocol: corev1.ProtocolTCP},
		{Name: "serfwan-udp", Port: 8302, TargetPort: intstr.FromString("serfwan-udp"), Protocol: corev1.ProtocolUDP},
		{Name: "server", Port: 8300, TargetPort: intstr.FromString("server"), Protocol: corev1.ProtocolTCP},
		{Name: "dns-tcp", Port: 8600, TargetPort: intstr.FromString("dns-tcp"), Protocol: corev1.ProtocolTCP},
		{Name: "dns-udp", Port: 8600, TargetPort: intstr.FromString("dns-udp"), Protocol: corev1.ProtocolUDP},
	}
}

func exposeServiceName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-expose"
}
