// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const k8sNamespace = "k8snamespace"

func TestHandlerContainerInit(t *testing.T) {
	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					constants.AnnotationService: "foo",
				},
			},

			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "web",
					},
					{
						Name: "web-side",
					},
				},
			},
			Status: corev1.PodStatus{
				HostIP: "1.1.1.1",
				PodIP:  "2.2.2.2",
			},
		}
	}

	cases := []struct {
		Name    string
		Pod     func(*corev1.Pod) *corev1.Pod
		Webhook MeshWebhook
		ExpCmd  string // Strings.Contains test
		ExpEnv  []corev1.EnvVar
	}{
		{
			"default cmd and env",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = "web"
				return pod
			},
			MeshWebhook{
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
				LogLevel:      "info",
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "0s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
			},
		},

		{
			"with auth method",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = "web"
				pod.Spec.ServiceAccountName = "a-service-account-name"
				pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "sa",
						MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
					},
				}
				return pod
			},
			MeshWebhook{
				AuthMethod:    "an-auth-method",
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
				LogLevel:      "debug",
				LogJSON:       true,
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=debug \
  -log-json=true \
  -service-account-name="a-service-account-name" \
  -service-name="web" \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
				{
					Name:  "CONSUL_LOGIN_AUTH_METHOD",
					Value: "an-auth-method",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_META",
					Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			w := tt.Webhook
			pod := *tt.Pod(minimal())
			container, err := w.containerInit(testNS, pod, multiPortInfo{})
			require.NoError(t, err)
			actual := strings.Join(container.Command, " ")
			require.Contains(t, actual, tt.ExpCmd)
			require.EqualValues(t, container.Env[3:], tt.ExpEnv)
		})
	}
}

func TestHandlerContainerInit_transparentProxy(t *testing.T) {
	cases := map[string]struct {
		globalEnabled    bool
		cniEnabled       bool
		annotations      map[string]string
		expTproxyEnabled bool
		namespaceLabel   map[string]string
		openShiftEnabled bool
	}{
		"enabled globally, ns not set, annotation not provided, cni disabled, openshift disabled": {
			true,
			false,
			nil,
			true,
			nil,
			false,
		},
		"enabled globally, ns not set, annotation is false, cni disabled, openshift disabled": {
			true,
			false,
			map[string]string{constants.KeyTransparentProxy: "false"},
			false,
			nil,
			false,
		},
		"enabled globally, ns not set, annotation is true, cni disabled, openshift disabled": {
			true,
			false,
			map[string]string{constants.KeyTransparentProxy: "true"},
			true,
			nil,
			false,
		},
		"disabled globally, ns not set, annotation not provided, cni disabled, openshift disabled": {
			false,
			false,
			nil,
			false,
			nil,
			false,
		},
		"disabled globally, ns not set, annotation is false, cni disabled, openshift disabled": {
			false,
			false,
			map[string]string{constants.KeyTransparentProxy: "false"},
			false,
			nil,
			false,
		},
		"disabled globally, ns not set, annotation is true, cni disabled, openshift disabled": {
			false,
			false,
			map[string]string{constants.KeyTransparentProxy: "true"},
			true,
			nil,
			false,
		},
		"disabled globally, ns enabled, annotation not set, cni disabled, openshift disabled": {
			false,
			false,
			nil,
			true,
			map[string]string{constants.KeyTransparentProxy: "true"},
			false,
		},
		"enabled globally, ns disabled, annotation not set, cni disabled, openshift disabled": {
			true,
			false,
			nil,
			false,
			map[string]string{constants.KeyTransparentProxy: "false"},
			false,
		},
		"disabled globally, ns enabled, annotation not set, cni enabled, openshift disabled": {
			false,
			true,
			nil,
			false,
			map[string]string{constants.KeyTransparentProxy: "true"},
			false,
		},

		"enabled globally, ns not set, annotation not set, cni enabled, openshift disabled": {
			true,
			true,
			nil,
			false,
			nil,
			false,
		},
		"enabled globally, ns not set, annotation not set, cni enabled, openshift enabled": {
			true,
			true,
			nil,
			false,
			nil,
			true,
		},
		"enabled globally, ns not set, annotation not set, cni disabled, openshift enabled": {
			true,
			false,
			nil,
			true,
			nil,
			true,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableTransparentProxy: c.globalEnabled,
				EnableCNI:              c.cniEnabled,
				ConsulConfig:           &consul.Config{HTTPPort: 8500},
				EnableOpenShift:        c.openShiftEnabled,
			}
			pod := minimal()
			pod.Annotations = c.annotations

			privileged := false
			if c.openShiftEnabled && !c.cniEnabled {
				privileged = true
			}

			var expectedSecurityContext *corev1.SecurityContext
			if c.cniEnabled && !c.openShiftEnabled {
				expectedSecurityContext = &corev1.SecurityContext{
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
					RunAsUser:    ptr.To(int64(initContainersUserAndGroupID)),
					RunAsGroup:   ptr.To(int64(initContainersUserAndGroupID)),
					RunAsNonRoot: ptr.To(true),
					Privileged:   ptr.To(privileged),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					ReadOnlyRootFilesystem:   ptr.To(true),
					AllowPrivilegeEscalation: ptr.To(false),
				}
			} else if c.expTproxyEnabled {
				expectedSecurityContext = &corev1.SecurityContext{
					RunAsUser:    ptr.To(int64(0)),
					RunAsGroup:   ptr.To(int64(0)),
					RunAsNonRoot: ptr.To(false),
					Privileged:   ptr.To(privileged),
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{netAdminCapability},
					},
				}
			} else if c.cniEnabled && c.openShiftEnabled {
				// When cni + openShift
				expectedSecurityContext = &corev1.SecurityContext{
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
					RunAsUser:    ptr.To(int64(1000799999)),
					RunAsGroup:   ptr.To(int64(1000799999)),
					RunAsNonRoot: ptr.To(true),
					Privileged:   ptr.To(privileged),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					ReadOnlyRootFilesystem:   ptr.To(true),
					AllowPrivilegeEscalation: ptr.To(false),
				}
			} else {
				// When tproxy disabled
				expectedSecurityContext = &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					Capabilities: &corev1.Capabilities{
						Add:  []corev1.Capability{},
						Drop: []corev1.Capability{"ALL"},
					},
					Privileged:   ptr.To(true),
					ReadOnlyRootFilesystem: ptr.To(true),
					RunAsNonRoot:           ptr.To(true),
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
				}
			}
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        k8sNamespace,
					Namespace:   k8sNamespace,
					Annotations: map[string]string{},
					Labels:      map[string]string{},
				},
			}

			if c.openShiftEnabled {
				ns.Annotations[constants.AnnotationOpenShiftUIDRange] = "1000700000/100000"
				ns.Annotations[constants.AnnotationOpenShiftGroups] = "1000700000/100000"
			}

			ns.Labels = c.namespaceLabel
			container, err := w.containerInit(ns, *pod, multiPortInfo{})
			require.NoError(t, err)

			redirectTrafficEnvVarFound := false
			for _, ev := range container.Env {
				if ev.Name == "CONSUL_REDIRECT_TRAFFIC_CONFIG" {
					redirectTrafficEnvVarFound = true
					break
				}
			}

			require.Equal(t, c.expTproxyEnabled, redirectTrafficEnvVarFound)
			require.Equal(t, expectedSecurityContext, container.SecurityContext)
		})
	}
}

func TestHandlerContainerInit_namespacesAndPartitionsEnabled(t *testing.T) {
	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.AnnotationService: "foo",
				},
			},

			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "web",
					},
					{
						Name: "web-side",
					},
					{
						Name: "auth-method-secret",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "service-account-secret",
								MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
							},
						},
					},
				},
				ServiceAccountName: "web",
			},
		}
	}

	cases := []struct {
		Name    string
		Pod     func(*corev1.Pod) *corev1.Pod
		Webhook MeshWebhook
		Cmd     string
		ExpEnv  []corev1.EnvVar
	}{
		{
			"default namespace, no partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				ConsulPartition:            "",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "default",
				},
			},
		},
		{
			"default namespace, default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				ConsulPartition:            "default",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "default",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "default",
				},
			},
		},
		{
			"non-default namespace, no partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
				ConsulPartition:            "",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "non-default",
				},
			},
		},
		{
			"non-default namespace, non-default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
				ConsulPartition:            "non-default-part",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "non-default-part",
				},
			},
		},
		{
			"auth method, non-default namespace, mirroring disabled, default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = ""
				return pod
			},
			MeshWebhook{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
				ConsulPartition:            "default",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \
  -service-account-name="web" \
  -service-name="" \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
				{
					Name:  "CONSUL_LOGIN_AUTH_METHOD",
					Value: "auth-method",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_META",
					Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
				},
				{
					Name:  "CONSUL_LOGIN_NAMESPACE",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_LOGIN_PARTITION",
					Value: "default",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "default",
				},
			},
		},
		{
			"auth method, non-default namespace, mirroring enabled, non-default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationService] = ""
				return pod
			},
			MeshWebhook{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default", // Overridden by mirroring
				EnableK8SNSMirroring:       true,
				ConsulPartition:            "non-default",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \
  -service-account-name="web" \
  -service-name="" \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NODE_NAME",
					Value: "$(NODE_NAME)-virtual",
				},
				{
					Name:  "CONSUL_LOGIN_AUTH_METHOD",
					Value: "auth-method",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_META",
					Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
				},
				{
					Name:  "CONSUL_LOGIN_NAMESPACE",
					Value: "default",
				},
				{
					Name:  "CONSUL_LOGIN_PARTITION",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "k8snamespace",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "non-default",
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			h := tt.Webhook
			h.LogLevel = "info"
			container, err := h.containerInit(testNS, *tt.Pod(minimal()), multiPortInfo{})
			require.NoError(t, err)
			actual := strings.Join(container.Command, " ")
			require.Equal(t, tt.Cmd, actual)
			if tt.ExpEnv != nil {
				require.Equal(t, tt.ExpEnv, container.Env[3:])
			}
		})
	}
}

func TestHandlerContainerInit_Multiport(t *testing.T) {
	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.AnnotationService: "web,web-admin",
				},
			},

			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "web-admin-service-account",
					},
				},
				Containers: []corev1.Container{
					{
						Name: "web",
					},
					{
						Name: "web-side",
					},
					{
						Name: "web-admin",
					},
					{
						Name: "web-admin-side",
					},
					{
						Name: "auth-method-secret",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "service-account-secret",
								MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
							},
						},
					},
				},
				ServiceAccountName: "web",
			},
		}
	}

	cases := []struct {
		Name              string
		Pod               func(*corev1.Pod) *corev1.Pod
		Webhook           MeshWebhook
		NumInitContainers int
		MultiPortInfos    []multiPortInfo
		Cmd               []string // Strings.Contains test
		ExpEnvVars        []corev1.EnvVar
	}{
		{
			"Whole template, multiport",
			func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MeshWebhook{
				LogLevel:      "info",
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			},
			2,
			[]multiPortInfo{
				{
					serviceIndex: 0,
					serviceName:  "web",
				},
				{
					serviceIndex: 1,
					serviceName:  "web-admin",
				},
			},
			[]string{
				`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web \
  -service-name="web" \`,

				`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web-admin \
  -service-name="web-admin" \`,
			},
			nil,
		},
		{
			"Whole template, multiport, auth method",
			func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MeshWebhook{
				AuthMethod:    "auth-method",
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
				LogLevel:      "info",
			},
			2,
			[]multiPortInfo{
				{
					serviceIndex: 0,
					serviceName:  "web",
				},
				{
					serviceIndex: 1,
					serviceName:  "web-admin",
				},
			},
			[]string{
				`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \
  -service-account-name="web" \
  -service-name="web" \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web \`,

				`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -log-level=info \
  -log-json=false \
  -service-account-name="web-admin" \
  -service-name="web-admin" \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web-admin \`,
			},
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/consul/serviceaccount-web-admin/token",
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			h := tt.Webhook
			for i := 0; i < tt.NumInitContainers; i++ {
				container, err := h.containerInit(testNS, *tt.Pod(minimal()), tt.MultiPortInfos[i])
				require.NoError(t, err)
				actual := strings.Join(container.Command, " ")
				require.Equal(t, tt.Cmd[i], actual)
				if tt.ExpEnvVars != nil {
					require.Contains(t, container.Env, tt.ExpEnvVars[i])
				}
			}
		})
	}
}

// If TLSEnabled is set,
// Consul addresses should use HTTPS
// and CA cert should be set as env variable if provided.
// Additionally, test that the init container is correctly configured
// when http or gRPC ports are different from defaults.
func TestHandlerContainerInit_WithTLSAndCustomPorts(t *testing.T) {
	for _, caProvided := range []bool{true, false} {
		name := fmt.Sprintf("ca provided: %t", caProvided)
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				ConsulAddress: "10.0.0.0",
				TLSEnabled:    true,
				ConsulConfig:  &consul.Config{HTTPPort: 443, GRPCPort: 8503},
			}
			if caProvided {
				w.ConsulCACert = "consul-ca-cert"
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService: "foo",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			container, err := w.containerInit(testNS, *pod, multiPortInfo{})
			require.NoError(t, err)
			require.Equal(t, "CONSUL_ADDRESSES", container.Env[3].Name)
			require.Equal(t, w.ConsulAddress, container.Env[3].Value)
			require.Equal(t, "CONSUL_GRPC_PORT", container.Env[4].Name)
			require.Equal(t, fmt.Sprintf("%d", w.ConsulConfig.GRPCPort), container.Env[4].Value)
			require.Equal(t, "CONSUL_HTTP_PORT", container.Env[5].Name)
			require.Equal(t, fmt.Sprintf("%d", w.ConsulConfig.HTTPPort), container.Env[5].Value)
			if w.TLSEnabled {
				require.Equal(t, "CONSUL_USE_TLS", container.Env[8].Name)
				require.Equal(t, "true", container.Env[8].Value)
				if caProvided {
					require.Equal(t, "CONSUL_CACERT_PEM", container.Env[9].Name)
					require.Equal(t, "consul-ca-cert", container.Env[9].Value)
				} else {
					for _, ev := range container.Env {
						if ev.Name == "CONSUL_CACERT_PEM" {
							require.Empty(t, ev.Value)
						}
					}
				}
			}
		})
	}
}

func TestHandlerContainerInit_Resources(t *testing.T) {
	w := MeshWebhook{
		InitContainerResources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("10Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("20m"),
				corev1.ResourceMemory: resource.MustParse("25Mi"),
			},
		},
		ConsulConfig: &consul.Config{HTTPPort: 8500, APITimeout: 5 * time.Second},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				constants.AnnotationService: "foo",
			},
		},

		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
		},
	}
	container, err := w.containerInit(testNS, *pod, multiPortInfo{})
	require.NoError(t, err)
	require.Equal(t, corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("20m"),
			corev1.ResourceMemory: resource.MustParse("25Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("10Mi"),
		},
	}, container.Resources)
}

var testNS = corev1.Namespace{
	ObjectMeta: metav1.ObjectMeta{
		Name:   k8sNamespace,
		Labels: map[string]string{},
	},
}

func minimal() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaces.DefaultNamespace,
			Name:      "minimal",
			Annotations: map[string]string{
				constants.AnnotationService: "foo",
			},
		},

		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
				{
					Name: "web-side",
				},
			},
		},
	}
}
