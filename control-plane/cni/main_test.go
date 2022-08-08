package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
			actual := skipTrafficRedirection(*c.annotatedPod(minimalPod()))
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

func TestClusterIPFromDNSService(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		fullName          string
		serviceName       string
		serviceNamespace  string
		configuredService func(string, string, string) *corev1.Service
		expectedIP        string
		expectedErr       error
	}{
		{
			name:             "Service with cluster IP set",
			fullName:         "consul-consul-dns.consul",
			serviceName:      "consul-consul-dns",
			serviceNamespace: "consul",
			expectedIP:       "10.0.0.1",
			configuredService: func(serviceName, serviceNamespace, IP string) *corev1.Service {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      serviceName,
						Namespace: serviceNamespace,
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: IP,
						Ports: []corev1.ServicePort{
							{
								Port: 8081,
							},
						},
					},
				}
				return service
			},
			expectedErr: nil,
		},
		{
			name:             "No service found",
			fullName:         "consul-consul-dns.consul",
			serviceName:      "consul-consul-dns",
			serviceNamespace: "consul",
			expectedIP:       "",
			configuredService: func(serviceName, serviceNamespace, IP string) *corev1.Service {
				return &corev1.Service{}
			},
			expectedErr: fmt.Errorf("unable to get service: services \"consul-consul-dns\" not found"),
		},
		{
			name:             "Service is missing IP",
			fullName:         "consul-consul-dns.consul",
			serviceName:      "consul-consul-dns",
			serviceNamespace: "consul",
			expectedIP:       "",
			configuredService: func(serviceName, serviceNamespace, IP string) *corev1.Service {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      serviceName,
						Namespace: serviceNamespace,
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Port: 8081,
							},
						},
					},
				}
				return service
			},
			expectedErr: fmt.Errorf("no cluster ip found on service: consul-consul-dns"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			service := c.configuredService(c.serviceName, c.serviceNamespace, c.expectedIP)
			_, err := client.CoreV1().Services(c.serviceNamespace).Create(context.Background(), service, metav1.CreateOptions{})
			require.NoError(t, err)

			actual, err := clusterIPFromDNSService(client, c.fullName)
			require.Equal(t, c.expectedErr, err)
			require.Equal(t, c.expectedIP, actual)
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
