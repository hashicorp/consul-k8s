package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSkipTrafficRedirection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		annotatedPod func(*corev1.Pod) *corev1.Pod
		retries      uint64
		expectedSkip bool
	}{
		{
			name: "Pod with both annotations correctly set",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[keyInjectStatus] = "foo"
				pod.Annotations[keyTransparentProxyStatus] = "bar"
				return pod
			},
			retries:      1,
			expectedSkip: false,
		},
		{
			name: "Pod without annotations, will timeout waiting",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			retries:      1,
			expectedSkip: true,
		},
		{
			name: "Pod only with connect-inject-status annotation, will timeout waiting for other annotation",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[keyInjectStatus] = "foo"
				return pod
			},
			retries:      1,
			expectedSkip: true,
		},
		{
			name: "Pod with only transparent-proxy-status annotation, will timeout waiting for other annotation",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[keyTransparentProxyStatus] = "foo"
				return pod
			},
			retries:      1,
			expectedSkip: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := skipTrafficRedirection(*c.annotatedPod(minimalPod()), c.retries)
			require.Equal(t, c.expectedSkip, actual)
		})
	}
}

func TestWaitForAnnotation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		annotation   string
		annotatedPod func(*corev1.Pod) *corev1.Pod
		retries      uint64
		exists       bool
	}{
		{
			name:       "Pod with annotation already existing",
			annotation: "fooAnnotation",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["fooAnnotation"] = "foo"
				return pod
			},
			retries: 1,
			exists:  true,
		},
		{
			name:       "Pod without annotation",
			annotation: "",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			retries: 1,
			exists:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := waitForAnnotation(*c.annotatedPod(minimalPod()), c.annotation, c.retries)
			require.Equal(t, c.exists, actual)
		})
	}
}

func TestParseAnnotation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		annotation   string
		configurePod func(*corev1.Pod) *corev1.Pod
		expected     iptables.Config
		err          error
	}{
		{
			name:       "Pod with iptables.Config annotation",
			annotation: annotationCNIProxyConfig,
			configurePod: func(pod *corev1.Pod) *corev1.Pod {
				// Use iptables.Config so that if the Config struct ever changes that the test is still valid
				cfg := iptables.Config{ProxyUserID: "1234"}
				j, err := json.Marshal(&cfg)
				if err != nil {
					t.Fatalf("could not marshal iptables config: %v", err)
				}
				pod.Annotations[annotationCNIProxyConfig] = string(j)
				return pod
			},
			expected: iptables.Config{
				ProxyUserID: "1234",
			},
			err: nil,
		},
		{
			name:       "Pod without iptables.Config annotation",
			annotation: annotationCNIProxyConfig,
			configurePod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			expected: iptables.Config{},
			err:      fmt.Errorf("could not find %s annotation for minimal pod", annotationCNIProxyConfig),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, err := parseAnnotation(*c.configurePod(minimalPod()), c.annotation)
			require.Equal(t, c.expected, actual)
			require.Equal(t, c.err, err)
		})
	}
}

func minimalPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "minimal",
			Annotations: map[string]string{},
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
