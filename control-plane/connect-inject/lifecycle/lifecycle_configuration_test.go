// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package lifecycle

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLifecycleConfig_EnableSidecarProxyLifecycle(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        bool
		Err             string
	}{
		{
			Name: "Sidecar proxy lifecycle management enabled via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableProxyLifecycle: true,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle management enabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableSidecarProxyLifecycle] = "true"
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableProxyLifecycle: false,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle management configured via invalid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableSidecarProxyLifecycle] = "not-a-bool"
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableProxyLifecycle: false,
			},
			Expected: false,
			Err:      "consul.hashicorp.com/enable-sidecar-proxy-lifecycle annotation value of not-a-bool was invalid: strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual, err := lc.EnableProxyLifecycle(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestLifecycleConfig_ShutdownDrainListeners(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        bool
		Err             string
	}{
		{
			Name: "Sidecar proxy shutdown listener draining enabled via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableShutdownDrainListeners: true,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Sidecar proxy shutdown listener draining enabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners] = "true"
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableShutdownDrainListeners: false,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Sidecar proxy shutdown listener draining configured via invalid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners] = "not-a-bool"
				return pod
			},
			Expected: false,
			Err:      "consul.hashicorp.com/enable-sidecar-proxy-lifecycle-shutdown-drain-listeners annotation value of not-a-bool was invalid: strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual, err := lc.EnableShutdownDrainListeners(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestLifecycleConfig_ShutdownGracePeriodSeconds(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        int
		Err             string
	}{
		{
			Name: "Sidecar proxy shutdown grace period set via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultShutdownGracePeriodSeconds: 10,
			},
			Expected: 10,
			Err:      "",
		},
		{
			Name: "Sidecar proxy shutdown grace period set via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds] = "20"
				return pod
			},
			LifecycleConfig: Config{
				DefaultShutdownGracePeriodSeconds: 10,
			},
			Expected: 20,
			Err:      "",
		},
		{
			Name: "Sidecar proxy shutdown grace period configured via invalid annotation, negative number",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds] = "-1"
				return pod
			},
			Err: "unable to parse annotation \"consul.hashicorp.com/sidecar-proxy-lifecycle-shutdown-grace-period-seconds\": strconv.ParseUint: parsing \"-1\": invalid syntax",
		},
		{
			Name: "Sidecar proxy shutdown grace period configured via invalid annotation, not-parseable string",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds] = "not-int"
				return pod
			},
			Err: "unable to parse annotation \"consul.hashicorp.com/sidecar-proxy-lifecycle-shutdown-grace-period-seconds\": strconv.ParseUint: parsing \"not-int\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual, err := lc.ShutdownGracePeriodSeconds(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestLifecycleConfig_StartupGracePeriodSeconds(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        int
		Err             string
	}{
		{
			Name: "Sidecar proxy startup grace period set via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultStartupGracePeriodSeconds: 10,
			},
			Expected: 10,
			Err:      "",
		},
		{
			Name: "Sidecar proxy startup grace period set via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleStartupGracePeriodSeconds] = "20"
				return pod
			},
			LifecycleConfig: Config{
				DefaultStartupGracePeriodSeconds: 10,
			},
			Expected: 20,
			Err:      "",
		},
		{
			Name: "Sidecar proxy startup grace period configured via invalid annotation, negative number",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleStartupGracePeriodSeconds] = "-1"
				return pod
			},
			Err: "unable to parse annotation \"consul.hashicorp.com/sidecar-proxy-lifecycle-startup-grace-period-seconds\": strconv.ParseUint: parsing \"-1\": invalid syntax",
		},
		{
			Name: "Sidecar proxy startup grace period configured via invalid annotation, not-parseable string",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleStartupGracePeriodSeconds] = "not-int"
				return pod
			},
			Err: "unable to parse annotation \"consul.hashicorp.com/sidecar-proxy-lifecycle-startup-grace-period-seconds\": strconv.ParseUint: parsing \"not-int\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual, err := lc.StartupGracePeriodSeconds(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestLifecycleConfig_GracefulPort(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        int
		Err             string
	}{
		{
			Name: "Sidecar proxy lifecycle graceful port set to default",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Expected: constants.DefaultGracefulPort,
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle graceful port set via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultGracefulPort: "3000",
			},
			Expected: 3000,
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle graceful port set via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleGracefulPort] = "9000"
				return pod
			},
			LifecycleConfig: Config{
				DefaultGracefulPort: "3000",
			},
			Expected: 9000,
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle graceful port configured via invalid annotation, negative number",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleGracefulPort] = "-1"
				return pod
			},
			Err: "consul.hashicorp.com/sidecar-proxy-lifecycle-graceful-port annotation value of -1 is not in the unprivileged port range 1024-65535",
		},
		{
			Name: "Sidecar proxy lifecycle graceful port configured via invalid annotation, not-parseable string",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleGracefulPort] = "not-int"
				return pod
			},
			Err: "consul.hashicorp.com/sidecar-proxy-lifecycle-graceful-port annotation value of not-int is not a valid integer",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual, err := lc.GracefulPort(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestLifecycleConfig_GracefulShutdownPath(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        string
		Err             string
	}{
		{
			Name: "Sidecar proxy lifecycle graceful shutdown path defaults to /graceful_shutdown",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Expected: "/graceful_shutdown",
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle graceful shutdown path set via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultGracefulShutdownPath: "/quit",
			},
			Expected: "/quit",
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle graceful port set via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleGracefulShutdownPath] = "/custom-shutdown-path"
				return pod
			},
			LifecycleConfig: Config{
				DefaultGracefulShutdownPath: "/quit",
			},
			Expected: "/custom-shutdown-path",
			Err:      "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual := lc.GracefulShutdownPath(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
}

func TestLifecycleConfig_GracefulStartupPath(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        string
		Err             string
	}{
		{
			Name: "Sidecar proxy lifecycle graceful startup path defaults to /graceful_startup",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Expected: "/graceful_startup",
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle graceful startup path set via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultGracefulStartupPath: "/start",
			},
			Expected: "/start",
			Err:      "",
		},
		{
			Name: "Sidecar proxy lifecycle graceful startup path set via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationSidecarProxyLifecycleGracefulStartupPath] = "/custom-startup-path"
				return pod
			},
			LifecycleConfig: Config{
				DefaultGracefulStartupPath: "/start",
			},
			Expected: "/custom-startup-path",
			Err:      "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual := lc.GracefulStartupPath(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
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

func TestLifecycleConfig_EnableConsulDataplaneAsSidecar(t *testing.T) {
	cases := []struct {
		Name            string
		Pod             func(*corev1.Pod) *corev1.Pod
		LifecycleConfig Config
		Expected        bool
		Err             string
	}{
		{
			Name: "Enabled via meshWebhook default",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableConsulDataplaneAsSidecar: true,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Enabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableConsulDataplaneAsSidecar] = "true"
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableConsulDataplaneAsSidecar: false,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Disabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableConsulDataplaneAsSidecar] = "false"
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableConsulDataplaneAsSidecar: true,
			},
			Expected: false,
			Err:      "",
		},
		{
			Name: "Invalid annotation value",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableConsulDataplaneAsSidecar] = "not-a-bool"
				return pod
			},
			LifecycleConfig: Config{
				DefaultEnableConsulDataplaneAsSidecar: false,
			},
			Expected: false,
			Err:      "consul.hashicorp.com/enable-consul-dataplane-as-sidecar annotation value of not-a-bool was invalid: strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			lc := tt.LifecycleConfig

			actual, err := lc.EnableConsulDataplaneAsSidecar(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}
