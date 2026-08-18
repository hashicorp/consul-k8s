// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	consulContainerName = "consul"

	defaultDataVolumeName = "data"

	// configChecksumAnnotation carries a hash of the generated server config so
	// that a config change produces a new pod template revision, which rolls the
	// servers. Consul only reloads a subset of its configuration at runtime, so
	// a restart is the only way to guarantee a change takes effect.
	configChecksumAnnotation = "consul.hashicorp.com/config-checksum"
)

// buildServerPodTemplate builds the pod template for the server StatefulSet.
// The data volume is supplied by the StatefulSet's volumeClaimTemplate, and
// hostname/subdomain come from the StatefulSet's serviceName, so neither is set
// here.
func buildServerPodTemplate(cluster *v1alpha1.ConsulCluster, configChecksum string) corev1.PodTemplateSpec {
	labels := serverPodLabels(cluster)
	labels[labelVersion] = cluster.Spec.Version

	if cluster.Spec.Pod != nil {
		for k, v := range cluster.Spec.Pod.Labels {
			labels[k] = v
		}
	}

	annotations := map[string]string{
		"consul.hashicorp.com/connect-inject": "false",
		"consul.hashicorp.com/mesh-inject":    "false",
		configChecksumAnnotation:              configChecksum,
	}
	if cluster.Spec.Metrics != nil && cluster.Spec.Metrics.Enabled {
		annotations["prometheus.io/scrape"] = "true"
		annotations["prometheus.io/path"] = "/v1/agent/metrics"
		if tlsEnabled(cluster) {
			annotations["prometheus.io/port"] = "8501"
			annotations["prometheus.io/scheme"] = "https"
		} else {
			annotations["prometheus.io/port"] = "8500"
			annotations["prometheus.io/scheme"] = "http"
		}
	}
	if cluster.Spec.Pod != nil {
		for k, v := range cluster.Spec.Pod.Annotations {
			annotations[k] = v
		}
	}

	env := buildServerEnv(cluster)
	args := buildServerArgs(cluster)

	image := consulImage(cluster)

	containerPorts := buildContainerPorts(cluster)

	volumeMounts := []corev1.VolumeMount{
		{Name: dataVolumeName(cluster), MountPath: "/consul/data"},
		{Name: "config", MountPath: "/consul/config"},
		{Name: "extra-config", MountPath: "/consul/extra-config"},
		// The container runs with a read-only root filesystem, so Consul needs a
		// writable /tmp for its own scratch files.
		{Name: "tmp", MountPath: "/tmp"},
	}

	volumes := []corev1.Volume{
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
			Name:         "extra-config",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name:         "tmp",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	}

	if tlsEnabled(cluster) {
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

	container := corev1.Container{
		Name:            consulContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		Command:         []string{"/bin/sh", "-ec"},
		Args:            []string{fmt.Sprintf("exec /bin/consul %s", strings.Join(args, " "))},
		Ports:           containerPorts,
		ReadinessProbe:  buildReadinessProbe(cluster),
		VolumeMounts:    volumeMounts,
		SecurityContext: serverSecurityContext(cluster),
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

	podSpec := corev1.PodSpec{
		Containers:                    containers,
		Volumes:                       volumes,
		RestartPolicy:                 corev1.RestartPolicyAlways,
		TerminationGracePeriodSeconds: int64Ptr(60),
		SecurityContext:               serverPodSecurityContext(cluster),
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

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: podSpec,
	}
}

func buildServerEnv(cluster *v1alpha1.ConsulCluster) []corev1.EnvVar {
	// When gossip and RPC ports are published as hostPorts the servers are
	// reachable at the node's address, not the pod's, so that is what they must
	// advertise. Advertising the pod IP would defeat the purpose of exposing the
	// ports at all, since external client agents cannot route to it.
	advertiseField := "status.podIP"
	if cluster.Spec.ExposeGossipAndRPCPorts {
		advertiseField = "status.hostIP"
	}

	env := []corev1.EnvVar{
		{
			Name: "ADVERTISE_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: advertiseField},
			},
		},
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
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		// The image entrypoint chowns /consul/data on startup, which fails under
		// a read-only root filesystem and a non-root user. We invoke the binary
		// directly, but set this so any wrapper in the image skips it too.
		{Name: "CONSUL_DISABLE_PERM_MGMT", Value: "true"},
	}

	if tlsEnabled(cluster) {
		env = append(env,
			corev1.EnvVar{Name: "CONSUL_HTTP_ADDR", Value: "https://127.0.0.1:8501"},
			corev1.EnvVar{Name: "CONSUL_CACERT", Value: "/consul/tls/ca/tls.crt"},
		)
	}

	if cluster.Spec.GossipEncryption != nil {
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

	if cluster.Spec.Pod != nil {
		env = append(env, cluster.Spec.Pod.ExtraEnvVars...)
	}

	return env
}

func buildServerArgs(cluster *v1alpha1.ConsulCluster) []string {
	// Everything that can live in the config file does, so that a change shows
	// up in the config checksum and rolls the servers. These flags cover only
	// what must be resolved from the pod's own environment.
	args := []string{
		"agent",
		"-advertise=$(ADVERTISE_IP)",
		"-config-dir=/consul/config",
		"-config-dir=/consul/extra-config",
	}

	if cluster.Spec.GossipEncryption != nil {
		args = append(args, "-encrypt=$(GOSSIP_KEY)")
	}

	// User-supplied config directories are loaded last so they can override
	// anything the operator generated.
	if cluster.Spec.Pod != nil {
		for _, mount := range cluster.Spec.Pod.ExtraVolumeMounts {
			if strings.HasPrefix(mount.MountPath, "/consul/userconfig/") {
				args = append(args, "-config-dir="+mount.MountPath)
			}
		}
	}

	return args
}

func buildContainerPorts(cluster *v1alpha1.ConsulCluster) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
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

	if !cluster.Spec.ExposeGossipAndRPCPorts {
		return ports
	}

	// Only gossip and RPC are published on the host; the HTTP and DNS ports are
	// reached through Services.
	hostPorted := map[string]bool{
		"serflan-tcp": true, "serflan-udp": true,
		"serfwan-tcp": true, "serfwan-udp": true,
		"server": true, "grpc": true,
	}
	for i := range ports {
		if hostPorted[ports[i].Name] {
			ports[i].HostPort = ports[i].ContainerPort
		}
	}
	return ports
}

// buildReadinessProbe returns a probe that only passes once the agent reports a
// leader. /v1/status/leader answers 200 with an empty body while an election is
// in progress, so an HTTP probe against it would call a leaderless server Ready.
// Grepping for a non-empty quoted address is what distinguishes the two.
func buildReadinessProbe(cluster *v1alpha1.ConsulCluster) *corev1.Probe {
	url := "http://127.0.0.1:8500/v1/status/leader"
	curl := "curl"
	if tlsEnabled(cluster) {
		url = "https://127.0.0.1:8501/v1/status/leader"
		curl = "curl -k"
	}

	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/sh", "-ec",
					fmt.Sprintf("%s %s 2>/dev/null | grep -E '\".+\"'", curl, url),
				},
			},
		},
		FailureThreshold:    2,
		InitialDelaySeconds: 5,
		PeriodSeconds:       3,
		SuccessThreshold:    1,
		TimeoutSeconds:      5,
	}
}

func serverSecurityContext(cluster *v1alpha1.ConsulCluster) *corev1.SecurityContext {
	if cluster.Spec.Pod != nil && cluster.Spec.Pod.ContainerSecurityContext != nil {
		return cluster.Spec.Pod.ContainerSecurityContext
	}
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(false),
		ReadOnlyRootFilesystem:   boolPtr(true),
		RunAsNonRoot:             boolPtr(true),
		RunAsUser:                int64Ptr(100),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{corev1.Capability("ALL")},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func serverPodSecurityContext(cluster *v1alpha1.ConsulCluster) *corev1.PodSecurityContext {
	if cluster.Spec.Pod != nil && cluster.Spec.Pod.SecurityContext != nil {
		return cluster.Spec.Pod.SecurityContext
	}
	return &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true)}
}

// dataVolumeName is the StatefulSet volume claim template name, which fixes the
// PVC name each ordinal binds to. Existing installations override it so their
// servers adopt the volumes they already have.
func dataVolumeName(cluster *v1alpha1.ConsulCluster) string {
	if cluster.Spec.DataVolumeName != "" {
		return cluster.Spec.DataVolumeName
	}
	return defaultDataVolumeName
}

func tlsEnabled(cluster *v1alpha1.ConsulCluster) bool {
	return cluster.Spec.TLS != nil && cluster.Spec.TLS.Enabled
}

func int64Ptr(i int64) *int64 { return &i }
