// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"testing"

	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

func Test_meshGatewayBuilder_Deployment(t *testing.T) {
	type fields struct {
		gateway *meshv2beta1.MeshGateway
		config  GatewayConfig
		gcc     *meshv2beta1.GatewayClassConfig
	}
	tests := []struct {
		name    string
		fields  fields
		want    *appsv1.Deployment
		wantErr bool
	}{
		{
			name: "happy path",
			fields: fields{
				gateway: &meshv2beta1.MeshGateway{
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
					},
				},
				config: GatewayConfig{},
				gcc: &meshv2beta1.GatewayClassConfig{
					Spec: meshv2beta1.GatewayClassConfigSpec{
						Deployment: meshv2beta1.GatewayClassDeploymentConfig{
							Replicas: &meshv2beta1.GatewayClassReplicasConfig{
								Default: pointer.Int32(1),
								Min:     pointer.Int32(1),
								Max:     pointer.Int32(8),
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
							},
							Annotations: map[string]string{
								"consul.hashicorp.com/mesh-inject": "false",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "consul-connect-inject-data",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{
											Medium: "Memory",
										},
									},
								},
							},
							InitContainers: []corev1.Container{
								{
									Name: "consul-connect-inject-init",
									Command: []string{
										"/bin/sh",
										"-ec",
										"consul-k8s-control-plane connect-init \\\n  -pod-name=${POD_NAME} \\\n  -pod-namespace=${POD_NAMESPACE} \\\n  -gateway-kind=\"mesh-gateway\" \\\n  -log-json=false \\\n  -service-name=\"\"",
									},
									Env: []corev1.EnvVar{
										{
											Name:  "POD_NAME",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "metadata.name",
												},
											},
										},
										{
											Name:  "POD_NAMESPACE",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "metadata.namespace",
												},
											},
										},
										{
											Name:  "NODE_NAME",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "spec.nodeName",
												},
											},
										},
										{
											Name:  "CONSUL_ADDRESSES",
											Value: "",
										},
										{
											Name:  "CONSUL_GRPC_PORT",
											Value: "0",
										},
										{
											Name:  "CONSUL_HTTP_PORT",
											Value: "0",
										},
										{
											Name:  "CONSUL_API_TIMEOUT",
											Value: "0s",
										},
										{
											Name:  "CONSUL_NODE_NAME",
											Value: "$(NODE_NAME)-virtual",
										},
										{
											Name:  "CONSUL_NAMESPACE",
											Value: "",
										},
									},
									Resources: corev1.ResourceRequirements{},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "consul-connect-inject-data",
											ReadOnly:  false,
											MountPath: "/consul/connect-inject",
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Args: []string{
										"-addresses",
										"",
										"-grpc-port=0",
										"-proxy-service-id-path=/consul/connect-inject/proxyid",
										"-log-level=",
										"-log-json=false",
										"-envoy-concurrency=1",
										"-tls-disabled",
										"-envoy-ready-bind-port=21000",
										"-envoy-admin-bind-port=19000",
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "proxy-health",
											ContainerPort: 21000,
										},
										{
											Name:          "wan",
											ContainerPort: 8443,
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "TMPDIR",
											Value: "/consul/connect-inject",
										},
										{
											Name:  "NODE_NAME",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "spec.nodeName",
												},
											},
										},
										{
											Name:  "DP_SERVICE_NODE_NAME",
											Value: "$(NODE_NAME)-virtual",
										},
										{
											Name:  "DP_ENVOY_READY_BIND_ADDRESS",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "status.podIP",
												},
											},
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "consul-connect-inject-data",
											MountPath: "/consul/connect-inject",
										},
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path: "/ready",
												Port: intstr.IntOrString{
													Type:   0,
													IntVal: 21000,
													StrVal: "",
												},
											},
										},
										InitialDelaySeconds: 1,
									},
									SecurityContext: &corev1.SecurityContext{
										Capabilities: &corev1.Capabilities{
											Add: []corev1.Capability{
												"NET_BIND_SERVICE",
											},
											Drop: []corev1.Capability{
												"ALL",
											},
										},
										RunAsNonRoot:             pointer.Bool(true),
										ReadOnlyRootFilesystem:   pointer.Bool(true),
										AllowPrivilegeEscalation: pointer.Bool(false),
										ProcMount:                nil,
										SeccompProfile:           nil,
									},
									Stdin:     false,
									StdinOnce: false,
									TTY:       false,
								},
							},
							Affinity: &corev1.Affinity{
								NodeAffinity: nil,
								PodAffinity:  nil,
								PodAntiAffinity: &corev1.PodAntiAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
										{
											Weight: 1,
											PodAffinityTerm: corev1.PodAffinityTerm{
												LabelSelector: &metav1.LabelSelector{
													MatchLabels: map[string]string{
														"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
													},
												},
												TopologyKey: "kubernetes.io/hostname",
											},
										},
									},
								},
							},
						},
					},
					Strategy:                appsv1.DeploymentStrategy{},
					MinReadySeconds:         0,
					RevisionHistoryLimit:    nil,
					Paused:                  false,
					ProgressDeadlineSeconds: nil,
				},
				Status: appsv1.DeploymentStatus{},
			},
			wantErr: false,
		},
		{
			name: "nil gatewayclassconfig - (notfound)",
			fields: fields{
				gateway: &meshv2beta1.MeshGateway{
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
					},
				},
				config: GatewayConfig{},
				gcc:    nil,
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
							},
							Annotations: map[string]string{
								"consul.hashicorp.com/mesh-inject": "false",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "consul-connect-inject-data",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{
											Medium: "Memory",
										},
									},
								},
							},
							InitContainers: []corev1.Container{
								{
									Name: "consul-connect-inject-init",
									Command: []string{
										"/bin/sh",
										"-ec",
										"consul-k8s-control-plane connect-init \\\n  -pod-name=${POD_NAME} \\\n  -pod-namespace=${POD_NAMESPACE} \\\n  -gateway-kind=\"mesh-gateway\" \\\n  -log-json=false \\\n  -service-name=\"\"",
									},
									Env: []corev1.EnvVar{
										{
											Name:  "POD_NAME",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "metadata.name",
												},
											},
										},
										{
											Name:  "POD_NAMESPACE",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "metadata.namespace",
												},
											},
										},
										{
											Name:  "NODE_NAME",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "spec.nodeName",
												},
											},
										},
										{
											Name:  "CONSUL_ADDRESSES",
											Value: "",
										},
										{
											Name:  "CONSUL_GRPC_PORT",
											Value: "0",
										},
										{
											Name:  "CONSUL_HTTP_PORT",
											Value: "0",
										},
										{
											Name:  "CONSUL_API_TIMEOUT",
											Value: "0s",
										},
										{
											Name:  "CONSUL_NODE_NAME",
											Value: "$(NODE_NAME)-virtual",
										},
										{
											Name:  "CONSUL_NAMESPACE",
											Value: "",
										},
									},
									Resources: corev1.ResourceRequirements{},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "consul-connect-inject-data",
											ReadOnly:  false,
											MountPath: "/consul/connect-inject",
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Args: []string{
										"-addresses",
										"",
										"-grpc-port=0",
										"-proxy-service-id-path=/consul/connect-inject/proxyid",
										"-log-level=",
										"-log-json=false",
										"-envoy-concurrency=1",
										"-tls-disabled",
										"-envoy-ready-bind-port=21000",
										"-envoy-admin-bind-port=19000",
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "proxy-health",
											ContainerPort: 21000,
										},
										{
											Name:          "wan",
											ContainerPort: 8443,
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "TMPDIR",
											Value: "/consul/connect-inject",
										},
										{
											Name:  "NODE_NAME",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "spec.nodeName",
												},
											},
										},
										{
											Name:  "DP_SERVICE_NODE_NAME",
											Value: "$(NODE_NAME)-virtual",
										},
										{
											Name:  "DP_ENVOY_READY_BIND_ADDRESS",
											Value: "",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "",
													FieldPath:  "status.podIP",
												},
											},
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "consul-connect-inject-data",
											MountPath: "/consul/connect-inject",
										},
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path: "/ready",
												Port: intstr.IntOrString{
													Type:   0,
													IntVal: 21000,
													StrVal: "",
												},
											},
										},
										InitialDelaySeconds: 1,
									},
									SecurityContext: &corev1.SecurityContext{
										Capabilities: &corev1.Capabilities{
											Add: []corev1.Capability{
												"NET_BIND_SERVICE",
											},
											Drop: []corev1.Capability{
												"ALL",
											},
										},
										RunAsNonRoot:             pointer.Bool(true),
										ReadOnlyRootFilesystem:   pointer.Bool(true),
										AllowPrivilegeEscalation: pointer.Bool(false),
										ProcMount:                nil,
										SeccompProfile:           nil,
									},
									Stdin:     false,
									StdinOnce: false,
									TTY:       false,
								},
							},
							Affinity: &corev1.Affinity{
								NodeAffinity: nil,
								PodAffinity:  nil,
								PodAntiAffinity: &corev1.PodAntiAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
										{
											Weight: 1,
											PodAffinityTerm: corev1.PodAffinityTerm{
												LabelSelector: &metav1.LabelSelector{
													MatchLabels: map[string]string{
														"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
													},
												},
												TopologyKey: "kubernetes.io/hostname",
											},
										},
									},
								},
							},
						},
					},
					Strategy:                appsv1.DeploymentStrategy{},
					MinReadySeconds:         0,
					RevisionHistoryLimit:    nil,
					Paused:                  false,
					ProgressDeadlineSeconds: nil,
				},
				Status: appsv1.DeploymentStatus{},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &meshGatewayBuilder{
				gateway: tt.fields.gateway,
				config:  tt.fields.config,
				gcc:     tt.fields.gcc,
			}
			got, err := b.Deployment()
			if !tt.wantErr && (err != nil) {
				assert.Errorf(t, err, "Error")
			}
			assert.Equalf(t, tt.want, got, "Deployment()")
		})
	}
}
