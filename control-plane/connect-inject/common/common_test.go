// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCommonDetermineAndValidatePort(t *testing.T) {
	cases := []struct {
		Name        string
		Pod         func(*corev1.Pod) *corev1.Pod
		Annotation  string
		Privileged  bool
		DefaultPort string
		Expected    string
		Err         string
	}{
		{
			Name: "Valid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "1234"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: false,
			Expected:   "1234",
			Err:        "",
		},
		{
			Name: "Uses default when there's no annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "4321",
			Expected:    "4321",
			Err:         "",
		},
		{
			Name: "Gets the value of the named default port when there's no annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          "web-port",
						ContainerPort: 2222,
					},
				}
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "web-port",
			Expected:    "2222",
			Err:         "",
		},
		{
			Name: "Errors if the named default port doesn't exist on the pod",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "web-port",
			Expected:    "",
			Err:         "web-port is not a valid port on the pod minimal",
		},
		{
			Name: "Gets the value of the named port",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "web-port"
				pod.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          "web-port",
						ContainerPort: 2222,
					},
				}
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "4321",
			Expected:    "2222",
			Err:         "",
		},
		{
			Name: "Invalid annotation (not an integer)",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "not-an-int"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: false,
			Expected:   "",
			Err:        "consul.hashicorp.com/test-annotation-port annotation value of not-an-int is not a valid integer",
		},
		{
			Name: "Invalid annotation (integer not in port range)",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "100000"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: true,
			Expected:   "",
			Err:        "consul.hashicorp.com/test-annotation-port annotation value of 100000 is not in the valid port range 1-65535",
		},
		{
			Name: "Invalid annotation (integer not in unprivileged port range)",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "22"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: false,
			Expected:   "",
			Err:        "consul.hashicorp.com/test-annotation-port annotation value of 22 is not in the unprivileged port range 1024-65535",
		},
		{
			Name: "Privileged ports allowed",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "22"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: true,
			Expected:   "22",
			Err:        "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			actual, err := DetermineAndValidatePort(*tt.Pod(minimal()), tt.Annotation, tt.DefaultPort, tt.Privileged)

			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.Expected, actual)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestPortValue(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      *corev1.Pod
		Value    string
		Expected int32
		Err      string
	}{
		{
			"empty",
			&corev1.Pod{},
			"",
			0,
			"strconv.ParseInt: parsing \"\": invalid syntax",
		},

		{
			"basic pod, with ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
						},

						{
							Name: "web-side",
						},
					},
				},
			},
			"http",
			int32(8080),
			"",
		},

		{
			"basic pod, with unnamed ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
								},
							},
						},

						{
							Name: "web-side",
						},
					},
				},
			},
			"8080",
			int32(8080),
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			port, err := PortValue(*tt.Pod, tt.Value)
			if (tt.Err != "") != (err != nil) {
				t.Fatalf("actual: %v, expected err: %v", err, tt.Err)
			}
			if tt.Err != "" {
				require.Contains(t, err.Error(), tt.Err)
				return
			}

			require.Equal(t, tt.Expected, port)
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
