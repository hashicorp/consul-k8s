// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"testing"

	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const testCert = `-----BEGIN CERTIFICATE-----                                                                                                                              │
MIIDQjCCAuigAwIBAgIUZGIigQ4IKLoCh4XrXyi/c89B7ZgwCgYIKoZIzj0EAwIw                                                                                         │
gZExCzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5j                                                                                         │
aXNjbzEaMBgGA1UECRMRMTAxIFNlY29uZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1                                                                                         │
MRcwFQYDVQQKEw5IYXNoaUNvcnAgSW5jLjEYMBYGA1UEAxMPQ29uc3VsIEFnZW50                                                                                         │
IENBMB4XDTI0MDEwMzE4NTYyOVoXDTMzMTIzMTE4NTcyOVowgZExCzAJBgNVBAYT                                                                                         │
AlVTMQswCQYDVQQIEwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5jaXNjbzEaMBgGA1UE                                                                                         │
CRMRMTAxIFNlY29uZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1MRcwFQYDVQQKEw5I                                                                                         │
YXNoaUNvcnAgSW5jLjEYMBYGA1UEAxMPQ29uc3VsIEFnZW50IENBMFkwEwYHKoZI                                                                                         │
zj0CAQYIKoZIzj0DAQcDQgAEcbkdpZxlDOEuT3ZCcZ8H9j0Jad8ncDYk/Y0IbHPC                                                                                         │
OKfFcpldEFPRv16WgSTHg38kK9WgEuK291+joBTHry3y06OCARowggEWMA4GA1Ud                                                                                         │
DwEB/wQEAwIBhjAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDwYDVR0T                                                                                         │
AQH/BAUwAwEB/zBoBgNVHQ4EYQRfZGY6MzA6YWE6NzI6ZTQ6ZTI6NzI6Y2Y6NTg6                                                                                         │
NDU6Zjk6YjU6NTA6N2I6ZDQ6MDI6MTE6ZjM6YzY6ZjE6NTc6NTE6MTg6NGU6OGU6                                                                                         │
ZjE6MmE6ZTE6MzI6NmY6ZTU6YjMwagYDVR0jBGMwYYBfZGY6MzA6YWE6NzI6ZTQ6                                                                                         │
ZTI6NzI6Y2Y6NTg6NDU6Zjk6YjU6NTA6N2I6ZDQ6MDI6MTE6ZjM6YzY6ZjE6NTc6                                                                                         │
NTE6MTg6NGU6OGU6ZjE6MmE6ZTE6MzI6NmY6ZTU6YjMwCgYIKoZIzj0EAwIDSAAw                                                                                         │
RQIgXg8YtejEgGNxswtyXsvqzhLpt7k44L7TJMUhfIw0lUECIQCIxKNowmv0/XVz                                                                                         │
nRnYLmGy79EZ2Y+CZS9nSm9Es6QNwg==                                                                                                                         │
-----END CERTIFICATE-----`

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
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							constants.AnnotationGatewayWANSource:  "Service",
							constants.AnnotationGatewayWANPort:    "443",
							constants.AnnotationGatewayWANAddress: "",
						},
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
					},
				},
				config: GatewayConfig{},
				gcc: &meshv2beta1.GatewayClassConfig{
					Spec: meshv2beta1.GatewayClassConfigSpec{
						GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
							Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
								Set: map[string]string{
									"app":      "consul",
									"chart":    "consul-helm",
									"heritage": "Helm",
									"release":  "consul",
								},
							},
							Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
								Set: map[string]string{
									"a": "b",
								},
							},
						},
						Deployment: meshv2beta1.GatewayClassDeploymentConfig{
							Affinity: &corev1.Affinity{
								PodAntiAffinity: &corev1.PodAntiAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
										{
											Weight: 1,
											PodAffinityTerm: corev1.PodAffinityTerm{
												LabelSelector: &metav1.LabelSelector{
													MatchLabels: map[string]string{
														labelManagedBy: "consul-k8s",
														"app":          "consul",
														"chart":        "consul-helm",
														"heritage":     "Helm",
														"release":      "consul",
													},
												},
												TopologyKey: "kubernetes.io/hostname",
											},
										},
									},
								},
							},
							GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
								Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
									Set: map[string]string{
										"foo": "bar",
									},
								},
								Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
									Set: map[string]string{
										"baz": "qux",
									},
								},
							},
							Container: &meshv2beta1.GatewayClassContainerConfig{
								HostPort:     8080,
								PortModifier: 8000,
								Consul: meshv2beta1.GatewayClassConsulConfig{
									Logging: meshv2beta1.GatewayClassConsulLoggingConfig{
										Level: "debug",
									},
								},
							},
							NodeSelector: map[string]string{"beta.kubernetes.io/arch": "amd64"},
							Replicas: &meshv2beta1.GatewayClassReplicasConfig{
								Default: ptr.To(int32(1)),
								Min:     ptr.To(int32(1)),
								Max:     ptr.To(int32(8)),
							},
							PriorityClassName: "priorityclassname",
							TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
								{
									MaxSkew:           1,
									TopologyKey:       "key",
									WhenUnsatisfiable: "DoNotSchedule",
								},
							},
							InitContainer: &meshv2beta1.GatewayClassInitContainerConfig{
								Resources: &corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										"cpu":    resource.MustParse("200m"),
										"memory": resource.MustParse("228Mi"),
									},
								},
								Consul: meshv2beta1.GatewayClassConsulConfig{
									Logging: meshv2beta1.GatewayClassConsulLoggingConfig{
										Level: "debug",
									},
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy: "consul-k8s",
						"app":          "consul",
						"chart":        "consul-helm",
						"heritage":     "Helm",
						"release":      "consul",
						"foo":          "bar",
					},
					Annotations: map[string]string{
						"a":   "b",
						"baz": "qux",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelManagedBy: "consul-k8s",
							"app":          "consul",
							"chart":        "consul-helm",
							"heritage":     "Helm",
							"release":      "consul",
							"foo":          "bar",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								labelManagedBy: "consul-k8s",
								"app":          "consul",
								"chart":        "consul-helm",
								"heritage":     "Helm",
								"foo":          "bar",
								"release":      "consul",
							},
							Annotations: map[string]string{
								constants.AnnotationGatewayKind:                     meshGatewayAnnotationKind,
								constants.AnnotationMeshInject:                      "false",
								constants.AnnotationTransparentProxyOverwriteProbes: "false",
								constants.AnnotationGatewayWANSource:                "Service",
								constants.AnnotationGatewayWANPort:                  "443",
								constants.AnnotationGatewayWANAddress:               "",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "consul-mesh-inject-data",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{
											Medium: "Memory",
										},
									},
								},
							},
							InitContainers: []corev1.Container{
								{
									Name: "consul-mesh-init",
									Command: []string{
										"/bin/sh",
										"-ec",
										"consul-k8s-control-plane mesh-init \\\n  -proxy-name=${POD_NAME} \\\n  -namespace=${POD_NAMESPACE} \\\n  -log-level=debug \\\n  -log-json=false",
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
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("128Mi"),
										},
										Limits: corev1.ResourceList{
											"cpu":    resource.MustParse("200m"),
											"memory": resource.MustParse("228Mi"),
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "consul-mesh-inject-data",
											ReadOnly:  false,
											MountPath: "/consul/mesh-inject",
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
										"-log-level=debug",
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
											HostPort:      8080,
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "DP_PROXY_ID",
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
											Name:  "TMPDIR",
											Value: "/consul/mesh-inject",
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
											Name:  "DP_CREDENTIAL_LOGIN_META",
											Value: "pod=$(POD_NAMESPACE)/$(DP_PROXY_ID)",
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
											Name:      "consul-mesh-inject-data",
											MountPath: "/consul/mesh-inject",
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
										RunAsNonRoot:             ptr.To(true),
										ReadOnlyRootFilesystem:   ptr.To(true),
										AllowPrivilegeEscalation: ptr.To(false),
										ProcMount:                nil,
										SeccompProfile:           nil,
									},
									Stdin:     false,
									StdinOnce: false,
									TTY:       false,
								},
							},
							NodeSelector:      map[string]string{"beta.kubernetes.io/arch": "amd64"},
							PriorityClassName: "priorityclassname",
							TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
								{
									MaxSkew:           1,
									TopologyKey:       "key",
									WhenUnsatisfiable: "DoNotSchedule",
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
														labelManagedBy: "consul-k8s",
														"app":          "consul",
														"chart":        "consul-helm",
														"heritage":     "Helm",
														"release":      "consul",
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
			name: "happy path tls enabled",
			fields: fields{
				gateway: &meshv2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							constants.AnnotationGatewayWANSource:  "Service",
							constants.AnnotationGatewayWANPort:    "443",
							constants.AnnotationGatewayWANAddress: "",
						},
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
					},
				},
				config: GatewayConfig{
					TLSEnabled:   true,
					ConsulCACert: testCert,
				},
				gcc: &meshv2beta1.GatewayClassConfig{
					Spec: meshv2beta1.GatewayClassConfigSpec{
						GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
							Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
								Set: map[string]string{
									"app":      "consul",
									"chart":    "consul-helm",
									"heritage": "Helm",
									"release":  "consul",
								},
							},
						},
						Deployment: meshv2beta1.GatewayClassDeploymentConfig{
							Affinity: &corev1.Affinity{
								PodAntiAffinity: &corev1.PodAntiAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
										{
											Weight: 1,
											PodAffinityTerm: corev1.PodAffinityTerm{
												LabelSelector: &metav1.LabelSelector{
													MatchLabels: map[string]string{
														labelManagedBy: "consul-k8s",
														"app":          "consul",
														"chart":        "consul-helm",
														"heritage":     "Helm",
														"release":      "consul",
													},
												},
												TopologyKey: "kubernetes.io/hostname",
											},
										},
									},
								},
							},
							Container: &meshv2beta1.GatewayClassContainerConfig{
								HostPort:     8080,
								PortModifier: 8000,
								Consul: meshv2beta1.GatewayClassConsulConfig{
									Logging: meshv2beta1.GatewayClassConsulLoggingConfig{
										Level: "debug",
									},
								},
							},
							NodeSelector: map[string]string{"beta.kubernetes.io/arch": "amd64"},
							Replicas: &meshv2beta1.GatewayClassReplicasConfig{
								Default: ptr.To(int32(1)),
								Min:     ptr.To(int32(1)),
								Max:     ptr.To(int32(8)),
							},
							PriorityClassName: "priorityclassname",
							TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
								{
									MaxSkew:           1,
									TopologyKey:       "key",
									WhenUnsatisfiable: "DoNotSchedule",
								},
							},
							InitContainer: &meshv2beta1.GatewayClassInitContainerConfig{
								Resources: &corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										"cpu":    resource.MustParse("200m"),
										"memory": resource.MustParse("228Mi"),
									},
								},
								Consul: meshv2beta1.GatewayClassConsulConfig{
									Logging: meshv2beta1.GatewayClassConsulLoggingConfig{
										Level: "debug",
									},
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy: "consul-k8s",
						"app":          "consul",
						"chart":        "consul-helm",
						"heritage":     "Helm",
						"release":      "consul",
					},

					Annotations: map[string]string{},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelManagedBy: "consul-k8s",
							"app":          "consul",
							"chart":        "consul-helm",
							"heritage":     "Helm",
							"release":      "consul",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								labelManagedBy: "consul-k8s",
								"app":          "consul",
								"chart":        "consul-helm",
								"heritage":     "Helm",
								"release":      "consul",
							},
							Annotations: map[string]string{
								constants.AnnotationGatewayKind:                     meshGatewayAnnotationKind,
								constants.AnnotationMeshInject:                      "false",
								constants.AnnotationTransparentProxyOverwriteProbes: "false",
								constants.AnnotationGatewayWANSource:                "Service",
								constants.AnnotationGatewayWANPort:                  "443",
								constants.AnnotationGatewayWANAddress:               "",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "consul-mesh-inject-data",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{
											Medium: "Memory",
										},
									},
								},
							},
							InitContainers: []corev1.Container{
								{
									Name: "consul-mesh-init",
									Command: []string{
										"/bin/sh",
										"-ec",
										"consul-k8s-control-plane mesh-init \\\n  -proxy-name=${POD_NAME} \\\n  -namespace=${POD_NAMESPACE} \\\n  -log-level=debug \\\n  -log-json=false",
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
										{
											Name:  "CONSUL_USE_TLS",
											Value: "true",
										},
										{
											Name:  "CONSUL_CACERT_PEM",
											Value: testCert,
										},
										{
											Name:  "CONSUL_TLS_SERVER_NAME",
											Value: "",
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											"cpu":    resource.MustParse("100m"),
											"memory": resource.MustParse("128Mi"),
										},
										Limits: corev1.ResourceList{
											"cpu":    resource.MustParse("200m"),
											"memory": resource.MustParse("228Mi"),
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "consul-mesh-inject-data",
											ReadOnly:  false,
											MountPath: "/consul/mesh-inject",
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
										"-log-level=debug",
										"-log-json=false",
										"-envoy-concurrency=1",
										"-ca-certs=/consul/mesh-inject/consul-ca.pem",
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
											HostPort:      8080,
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "DP_PROXY_ID",
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
											Name:  "TMPDIR",
											Value: "/consul/mesh-inject",
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
											Name:  "DP_CREDENTIAL_LOGIN_META",
											Value: "pod=$(POD_NAMESPACE)/$(DP_PROXY_ID)",
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
											Name:      "consul-mesh-inject-data",
											MountPath: "/consul/mesh-inject",
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
										RunAsNonRoot:             ptr.To(true),
										ReadOnlyRootFilesystem:   ptr.To(true),
										AllowPrivilegeEscalation: ptr.To(false),
										ProcMount:                nil,
										SeccompProfile:           nil,
									},
									Stdin:     false,
									StdinOnce: false,
									TTY:       false,
								},
							},
							NodeSelector:      map[string]string{"beta.kubernetes.io/arch": "amd64"},
							PriorityClassName: "priorityclassname",
							TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
								{
									MaxSkew:           1,
									TopologyKey:       "key",
									WhenUnsatisfiable: "DoNotSchedule",
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
														labelManagedBy: "consul-k8s",
														"app":          "consul",
														"chart":        "consul-helm",
														"heritage":     "Helm",
														"release":      "consul",
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
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							constants.AnnotationGatewayWANSource:  "Service",
							constants.AnnotationGatewayWANPort:    "443",
							constants.AnnotationGatewayWANAddress: "",
						},
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
					},
				},
				config: GatewayConfig{},
				gcc:    nil,
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      defaultLabels,
					Annotations: map[string]string{},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: defaultLabels,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: defaultLabels,
							Annotations: map[string]string{
								constants.AnnotationGatewayKind:                     meshGatewayAnnotationKind,
								constants.AnnotationMeshInject:                      "false",
								constants.AnnotationTransparentProxyOverwriteProbes: "false",
								constants.AnnotationGatewayWANSource:                "Service",
								constants.AnnotationGatewayWANPort:                  "443",
								constants.AnnotationGatewayWANAddress:               "",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "consul-mesh-inject-data",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{
											Medium: "Memory",
										},
									},
								},
							},
							InitContainers: []corev1.Container{
								{
									Name: "consul-mesh-init",
									Command: []string{
										"/bin/sh",
										"-ec",
										"consul-k8s-control-plane mesh-init \\\n  -proxy-name=${POD_NAME} \\\n  -namespace=${POD_NAMESPACE} \\\n  -log-json=false",
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
											Name:      "consul-mesh-inject-data",
											ReadOnly:  false,
											MountPath: "/consul/mesh-inject",
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
											ContainerPort: 443,
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "DP_PROXY_ID",
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
											Name:  "TMPDIR",
											Value: "/consul/mesh-inject",
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
											Name:  "DP_CREDENTIAL_LOGIN_META",
											Value: "pod=$(POD_NAMESPACE)/$(DP_PROXY_ID)",
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
											Name:      "consul-mesh-inject-data",
											MountPath: "/consul/mesh-inject",
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
										RunAsNonRoot:             ptr.To(true),
										ReadOnlyRootFilesystem:   ptr.To(true),
										AllowPrivilegeEscalation: ptr.To(false),
										ProcMount:                nil,
										SeccompProfile:           nil,
									},
									Stdin:     false,
									StdinOnce: false,
									TTY:       false,
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

func Test_MergeDeployment(t *testing.T) {
	testCases := []struct {
		name     string
		a, b     *appsv1.Deployment
		assertFn func(*testing.T, *appsv1.Deployment)
	}{
		{
			name: "new deployment gets desired annotations + labels + containers",
			a:    &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "deployment"}},
			b: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Namespace:   "default",
				Name:        "deployment",
				Annotations: map[string]string{"b": "b"},
				Labels:      map[string]string{"b": "b"},
			}},
			assertFn: func(t *testing.T, result *appsv1.Deployment) {
				assert.Equal(t, map[string]string{"b": "b"}, result.Annotations)
				assert.Equal(t, map[string]string{"b": "b"}, result.Labels)
			},
		},
		{
			name: "existing deployment keeps existing annotations + labels and gains desired annotations + labels + containers",
			a: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Namespace:         "default",
				Name:              "deployment",
				CreationTimestamp: metav1.Now(),
				Annotations:       map[string]string{"a": "a"},
				Labels:            map[string]string{"a": "a"},
			}},
			b: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "default",
					Name:        "deployment",
					Annotations: map[string]string{"b": "b"},
					Labels:      map[string]string{"b": "b"},
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "b"}},
						},
					},
				},
			},
			assertFn: func(t *testing.T, result *appsv1.Deployment) {
				assert.Equal(t, map[string]string{"a": "a", "b": "b"}, result.Annotations)
				assert.Equal(t, map[string]string{"a": "a", "b": "b"}, result.Labels)

				require.Len(t, result.Spec.Template.Spec.Containers, 1)
				assert.Equal(t, "b", result.Spec.Template.Spec.Containers[0].Name)
			},
		},
		{
			name: "existing deployment with injected initContainer retains it",
			a: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         "default",
					Name:              "deployment",
					CreationTimestamp: metav1.Now(),
					Annotations:       map[string]string{"a": "a"},
					Labels:            map[string]string{"a": "a"},
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{{Name: "b"}},
							Containers:     []corev1.Container{{Name: "b"}},
						},
					},
				},
			},
			b: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "default",
					Name:        "deployment",
					Annotations: map[string]string{"b": "b"},
					Labels:      map[string]string{"b": "b"},
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "b"}},
						},
					},
				},
			},
			assertFn: func(t *testing.T, result *appsv1.Deployment) {
				assert.Equal(t, map[string]string{"a": "a", "b": "b"}, result.Annotations)
				assert.Equal(t, map[string]string{"a": "a", "b": "b"}, result.Labels)

				require.Len(t, result.Spec.Template.Spec.InitContainers, 1)
				assert.Equal(t, "b", result.Spec.Template.Spec.InitContainers[0].Name)

				require.Len(t, result.Spec.Template.Spec.Containers, 1)
				assert.Equal(t, "b", result.Spec.Template.Spec.Containers[0].Name)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			MergeDeployment(testCase.a, testCase.b)
			testCase.assertFn(t, testCase.a)
		})
	}
}
