// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func (r *ConsulClusterReconciler) createServerPod(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	podName, err := generatePodName(cluster.Name)
	if err != nil {
		return err
	}

	pvc, err := r.ensurePVC(ctx, cluster, podName)
	if err != nil {
		return fmt.Errorf("creating PVC for pod %s: %w", podName, err)
	}

	pod := buildServerPod(cluster, podName, pvc.Name)
	pod.OwnerReferences = []metav1.OwnerReference{ownerRef(cluster, r.Scheme)}

	if err := r.Create(ctx, pod); err != nil {
		return fmt.Errorf("creating pod %s: %w", podName, err)
	}
	return nil
}

func (r *ConsulClusterReconciler) ensurePVC(ctx context.Context, cluster *v1alpha1.ConsulCluster, podName string) (*corev1.PersistentVolumeClaim, error) {
	pvcName := pvcNameForPod(podName)
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: pvcName}, pvc)
	if err == nil {
		return pvc, nil
	}
	if !k8serrors.IsNotFound(err) {
		return nil, err
	}

	storageQty := cluster.Spec.Storage
	if storageQty.IsZero() {
		storageQty = resource.MustParse("10Gi")
	}

	newPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cluster.Namespace,
			Labels:    serverPodLabels(cluster),
			OwnerReferences: []metav1.OwnerReference{
				ownerRef(cluster, r.Scheme),
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageQty,
				},
			},
			StorageClassName: cluster.Spec.StorageClassName,
		},
	}

	if err := ctrl.SetControllerReference(cluster, newPVC, r.Scheme); err != nil {
		return nil, err
	}

	if err := r.Create(ctx, newPVC); err != nil {
		return nil, err
	}
	return newPVC, nil
}

func buildServerPod(cluster *v1alpha1.ConsulCluster, podName, pvcName string) *corev1.Pod {
	labels := serverPodLabels(cluster)
	labels[labelVersion] = cluster.Spec.Version

	if cluster.Spec.Pod != nil {
		for k, v := range cluster.Spec.Pod.Labels {
			labels[k] = v
		}
	}

	annotations := map[string]string{
		"consul.hashicorp.com/connect-inject": "false",
	}
	if cluster.Spec.Metrics != nil && cluster.Spec.Metrics.Enabled {
		annotations["prometheus.io/scrape"] = "true"
		annotations["prometheus.io/port"] = "8500"
		annotations["prometheus.io/path"] = "/v1/agent/metrics"
	}
	if cluster.Spec.Pod != nil {
		for k, v := range cluster.Spec.Pod.Annotations {
			annotations[k] = v
		}
	}

	bootstrapExpect := cluster.Spec.Size
	if cluster.Spec.BootstrapExpect != nil {
		bootstrapExpect = *cluster.Spec.BootstrapExpect
	}

	datacenter := cluster.Spec.DatacenterName
	if datacenter == "" {
		datacenter = "dc1"
	}

	retryJoin := fmt.Sprintf("%s.%s.svc", headlessServiceName(cluster), cluster.Namespace)

	env := []corev1.EnvVar{
		{
			Name: "ADVERTISE_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		},
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
	}
	if cluster.Spec.Pod != nil {
		env = append(env, cluster.Spec.Pod.ExtraEnvVars...)
	}

	args := []string{
		"agent",
		"-advertise=$(ADVERTISE_IP)",
		"-bind=0.0.0.0",
		"-bootstrap-expect=" + fmt.Sprintf("%d", bootstrapExpect),
		"-client=0.0.0.0",
		"-config-dir=/consul/config",
		"-datacenter=" + datacenter,
		"-data-dir=/consul/data",
		"-domain=cluster.local",
		"-retry-join=" + retryJoin,
		"-server",
		"-ui",
	}

	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Enabled {
		args = append(args, "-config-dir=/consul/tls-config")
	}

	if cluster.Spec.GossipEncryption != nil {
		args = append(args, "-encrypt=$(GOSSIP_KEY)")
		env = append(env, corev1.EnvVar{
			Name: "GOSSIP_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cluster.Spec.GossipEncryption.SecretName,
					},
					Key: cluster.Spec.GossipEncryption.SecretKey,
				},
			},
		})
	}

	image := consulImage(cluster)

	containerPorts := []corev1.ContainerPort{
		{Name: "http", ContainerPort: 8500, Protocol: corev1.ProtocolTCP},
		{Name: "https", ContainerPort: 8501, Protocol: corev1.ProtocolTCP},
		{Name: "grpc", ContainerPort: 8502, Protocol: corev1.ProtocolTCP},
		{Name: "serflan-tcp", ContainerPort: 8301, Protocol: corev1.ProtocolTCP},
		{Name: "serflan-udp", ContainerPort: 8301, Protocol: corev1.ProtocolUDP},
		{Name: "serfwan-tcp", ContainerPort: 8302, Protocol: corev1.ProtocolTCP},
		{Name: "serfwan-udp", ContainerPort: 8302, Protocol: corev1.ProtocolUDP},
		{Name: "server", ContainerPort: 8300, Protocol: corev1.ProtocolTCP},
		{Name: "dns-tcp", ContainerPort: 8600, Protocol: corev1.ProtocolTCP},
		{Name: "dns-udp", ContainerPort: 8600, Protocol: corev1.ProtocolUDP},
	}

	if cluster.Spec.ExposeGossipAndRPCPorts {
		for i := range containerPorts {
			containerPorts[i].HostPort = containerPorts[i].ContainerPort
		}
	}

	readinessScheme := corev1.URISchemeHTTP
	readinessPort := intstr.FromString("http")
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Enabled && !cluster.Spec.TLS.HTTPSOnly {
		readinessScheme = corev1.URISchemeHTTPS
		readinessPort = intstr.FromString("https")
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/v1/status/leader",
				Port:   readinessPort,
				Scheme: readinessScheme,
			},
		},
		FailureThreshold:    3,
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      5,
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "data", MountPath: "/consul/data"},
		{Name: "config", MountPath: "/consul/config"},
		{Name: "extra-config", MountPath: "/consul/extra-config"},
	}

	volumes := []corev1.Volume{
		{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName(cluster),
					},
				},
			},
		},
		{
			Name: "extra-config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Enabled {
		volumes = append(volumes,
			corev1.Volume{
				Name: "tls-ca",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cluster.Spec.TLS.CASecretName,
					},
				},
			},
			corev1.Volume{
				Name: "tls-certs",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cluster.Spec.TLS.ServerCertSecretName,
					},
				},
			},
		)
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{Name: "tls-ca", MountPath: "/consul/tls/ca", ReadOnly: true},
			corev1.VolumeMount{Name: "tls-certs", MountPath: "/consul/tls/server", ReadOnly: true},
		)
	}

	if cluster.Spec.Pod != nil {
		volumeMounts = append(volumeMounts, cluster.Spec.Pod.ExtraVolumeMounts...)
		volumes = append(volumes, cluster.Spec.Pod.ExtraVolumes...)
	}

	drop := corev1.Capability("ALL")
	defaultSecurityContext := &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(false),
		ReadOnlyRootFilesystem:   boolPtr(true),
		RunAsNonRoot:             boolPtr(true),
		RunAsUser:                int64Ptr(100),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{drop},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	securityContext := defaultSecurityContext
	if cluster.Spec.Pod != nil && cluster.Spec.Pod.ContainerSecurityContext != nil {
		securityContext = cluster.Spec.Pod.ContainerSecurityContext
	}

	container := corev1.Container{
		Name:            "consul",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		Command:         []string{"/bin/sh", "-ec"},
		Args: []string{
			fmt.Sprintf(`exec /bin/consul %s`, joinArgs(args)),
		},
		Ports:           containerPorts,
		ReadinessProbe:  readinessProbe,
		VolumeMounts:    volumeMounts,
		SecurityContext: securityContext,
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", "consul leave"},
				},
			},
		},
	}

	if cluster.Spec.Pod != nil && cluster.Spec.Pod.Resources != nil {
		container.Resources = *cluster.Spec.Pod.Resources
	}

	containers := []corev1.Container{container}
	if cluster.Spec.Pod != nil {
		containers = append(containers, cluster.Spec.Pod.ExtraContainers...)
	}

	defaultPodSecurityContext := &corev1.PodSecurityContext{
		RunAsNonRoot: boolPtr(true),
	}
	podSecurityContext := defaultPodSecurityContext
	if cluster.Spec.Pod != nil && cluster.Spec.Pod.SecurityContext != nil {
		podSecurityContext = cluster.Spec.Pod.SecurityContext
	}

	podSpec := corev1.PodSpec{
		Containers:                    containers,
		Volumes:                       volumes,
		Hostname:                      podName,
		Subdomain:                     headlessServiceName(cluster),
		RestartPolicy:                 corev1.RestartPolicyAlways,
		TerminationGracePeriodSeconds: int64Ptr(60),
		SecurityContext:               podSecurityContext,
		PriorityClassName:             cluster.Spec.PriorityClassName,
		ServiceAccountName:            serviceAccountName(cluster),
	}

	if cluster.Spec.Pod != nil {
		podSpec.NodeSelector = cluster.Spec.Pod.NodeSelector
		podSpec.Affinity = cluster.Spec.Pod.Affinity
		podSpec.Tolerations = cluster.Spec.Pod.Tolerations
		podSpec.TopologySpreadConstraints = cluster.Spec.Pod.TopologySpreadConstraints
		podSpec.ImagePullSecrets = cluster.Spec.Pod.ImagePullSecrets
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: podSpec,
	}

	return pod
}

func generatePodName(clusterName string) (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating pod name suffix: %w", err)
	}
	return fmt.Sprintf("%s-server-%s", clusterName, hex.EncodeToString(b)), nil
}

func joinArgs(args []string) string {
	result := ""
	for _, a := range args {
		if result != "" {
			result += " "
		}
		result += a
	}
	return result
}

func int64Ptr(i int64) *int64 { return &i }
