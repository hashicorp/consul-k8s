package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	defaultPodName   = "fakePod"
	defaultNamespace = "default"
)

type fakeIptablesProvider struct {
	rules []string
}

func (f *fakeIptablesProvider) AddRule(name string, args ...string) {
	var rule []string
	rule = append(rule, name)
	rule = append(rule, args...)

	f.rules = append(f.rules, strings.Join(rule, " "))
}

func (f *fakeIptablesProvider) ApplyRules() error {
	return nil
}

func (f *fakeIptablesProvider) Rules() []string {
	return f.rules
}

func Test_cmdAdd(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		cmd           *Command
		podName       string
		stdInData     string
		configuredPod func(*corev1.Pod, *Command) *corev1.Pod
		expectedRules bool
		expectedErr   error
	}{
		{
			name:      "K8S_POD_NAME missing from CNI args, should throw error",
			cmd:       &Command{},
			podName:   "",
			stdInData: goodStdinData,
			configuredPod: func(pod *corev1.Pod, cmd *Command) *corev1.Pod {
				return pod
			},
			expectedErr:   fmt.Errorf("not running in a pod, namespace and pod should have values"),
			expectedRules: false, // Rules won't be applied because the command will throw an error first
		},
		{
			name: "Missing prevResult in stdin data, should throw error",
			cmd: &Command{
				client: fake.NewSimpleClientset(),
			},
			podName:   "missing-prev-result",
			stdInData: missingPrevResultStdinData,
			configuredPod: func(pod *corev1.Pod, cmd *Command) *corev1.Pod {
				_, err := cmd.client.CoreV1().Pods(defaultNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				return pod
			},
			expectedErr:   fmt.Errorf("must be called as final chained plugin"),
			expectedRules: false, // Rules won't be applied because the command will throw an error first
		},
		{
			name: "Missing IPs in prevResult in stdin data, should throw error",
			cmd: &Command{
				client: fake.NewSimpleClientset(),
			},
			podName:   "corrupt-prev-result",
			stdInData: missingIPsStdinData,
			configuredPod: func(pod *corev1.Pod, cmd *Command) *corev1.Pod {
				_, err := cmd.client.CoreV1().Pods(defaultNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				return pod
			},
			expectedErr:   fmt.Errorf("got no container IPs"),
			expectedRules: false, // Rules won't be applied because the command will throw an error first
		},
		{
			name: "Pod with incorrect traffic redirection annotation, should throw error",
			cmd: &Command{
				client: fake.NewSimpleClientset(),
			},
			podName:   "pod-with-incorrect-annotation",
			stdInData: goodStdinData,
			configuredPod: func(pod *corev1.Pod, cmd *Command) *corev1.Pod {
				pod.Annotations[keyInjectStatus] = "true"
				pod.Annotations[keyTransparentProxyStatus] = "enabled"
				pod.Annotations[annotationRedirectTraffic] = "{foo}"
				_, err := cmd.client.CoreV1().Pods(defaultNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				return pod
			},
			expectedErr:   fmt.Errorf("could not unmarshal %s annotation for %s pod", annotationRedirectTraffic, "pod-with-incorrect-annotation"),
			expectedRules: false, // Rules won't be applied because the command will throw an error first
		},
		{
			name: "Pod with correct annotations, should create redirect traffic rules",
			cmd: &Command{
				client:           fake.NewSimpleClientset(),
				iptablesProvider: &fakeIptablesProvider{},
			},
			podName:   "pod-no-proxy-outbound-port",
			stdInData: goodStdinData,
			configuredPod: func(pod *corev1.Pod, cmd *Command) *corev1.Pod {
				pod.Annotations[keyInjectStatus] = "true"
				pod.Annotations[keyTransparentProxyStatus] = "enabled"
				cfg := iptables.Config{
					ProxyUserID:      "123",
					ProxyInboundPort: 20000,
				}
				iptablesConfigJson, err := json.Marshal(&cfg)
				require.NoError(t, err)
				pod.Annotations[annotationRedirectTraffic] = string(iptablesConfigJson)
				_, err = cmd.client.CoreV1().Pods(defaultNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				return pod
			},
			expectedErr:   nil,
			expectedRules: true, // Rules will be applied
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_ = c.configuredPod(minimalPod(c.podName), c.cmd)
			err := c.cmd.cmdAdd(minimalSkelArgs(c.podName, defaultNamespace, c.stdInData))
			require.Equal(t, c.expectedErr, err)

			// Check to see that rules have been generated
			if c.expectedErr == nil && c.expectedRules {
				require.NotEmpty(t, c.cmd.iptablesProvider.Rules())
			}
		})
	}
}

func TestSkipTrafficRedirection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		annotatedPod func(*corev1.Pod) *corev1.Pod
		expectedSkip bool
	}{
		{
			name: "Pod with both annotations correctly set",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[keyInjectStatus] = "foo"
				pod.Annotations[keyTransparentProxyStatus] = "bar"
				return pod
			},
			expectedSkip: false,
		},
		{
			name: "Pod without annotations, will timeout waiting",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			expectedSkip: true,
		},
		{
			name: "Pod only with connect-inject-status annotation will skip because missing other annotation",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[keyInjectStatus] = "foo"
				return pod
			},
			expectedSkip: true,
		},
		{
			name: "Pod with only transparent-proxy-status annotation will skip because missing other annotation",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[keyTransparentProxyStatus] = "foo"
				return pod
			},
			expectedSkip: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := skipTrafficRedirection(*c.annotatedPod(minimalPod(defaultPodName)))
			require.Equal(t, c.expectedSkip, actual)
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
			annotation: annotationRedirectTraffic,
			configurePod: func(pod *corev1.Pod) *corev1.Pod {
				// Use iptables.Config so that if the Config struct ever changes that the test is still valid
				cfg := iptables.Config{ProxyUserID: "1234"}
				j, err := json.Marshal(&cfg)
				if err != nil {
					t.Fatalf("could not marshal iptables config: %v", err)
				}
				pod.Annotations[annotationRedirectTraffic] = string(j)
				return pod
			},
			expected: iptables.Config{
				ProxyUserID: "1234",
			},
			err: nil,
		},
		{
			name:       "Pod without iptables.Config annotation",
			annotation: annotationRedirectTraffic,
			configurePod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			expected: iptables.Config{},
			err:      fmt.Errorf("could not find %s annotation for %s pod", annotationRedirectTraffic, defaultPodName),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, err := parseAnnotation(*c.configurePod(minimalPod(defaultPodName)), c.annotation)
			require.Equal(t, c.expected, actual)
			require.Equal(t, c.err, err)
		})
	}
}

func minimalPod(podName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   defaultNamespace,
			Name:        podName,
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

func minimalSkelArgs(podName, namespace, stdinData string) *skel.CmdArgs {
	return &skel.CmdArgs{
		ContainerID: "some-container-id",
		Netns:       "/some/netns/path",
		IfName:      "eth0",
		Args:        fmt.Sprintf("K8S_POD_NAME=%s;K8S_POD_NAMESPACE=%s", podName, namespace),
		Path:        "/some/bin/path",
		StdinData:   []byte(stdinData),
	}
}

const goodStdinData = `{
    "cniVersion": "0.3.1",
	"name": "kindnet",
	"type": "kindnet",
    "capabilities": {
        "testCapability": false
    },
    "ipam": {
        "type": "host-local"
    },
    "dns": {
        "nameservers": ["nameserver"],
        "domain": "domain",
        "search": ["search"],
        "options": ["option"]
    },
    "prevResult": {
        "cniversion": "0.3.1",
        "interfaces": [
            {
                "name": "eth0",
                "sandbox": "/tmp"
            }
        ],
        "ips": [
            {
                "version": "4",
                "address": "10.0.0.2/24",
                "gateway": "10.0.0.1",
                "interface": 0
            }
        ],
        "routes": []

    },
    "cni_bin_dir": "/opt/cni/bin",
    "cni_net_dir": "/etc/cni/net.d",
    "kubeconfig": "ZZZ-consul-cni-kubeconfig",
    "log_level": "info",
    "multus": false,
    "name": "consul-cni",
    "type": "consul-cni"
}`

const missingPrevResultStdinData = `{
    "cniVersion": "0.3.1",
	"name": "kindnet",
	"type": "kindnet",
    "capabilities": {
        "testCapability": false
    },
    "ipam": {
        "type": "host-local"
    },
    "dns": {
        "nameservers": ["nameserver"],
        "domain": "domain",
        "search": ["search"],
        "options": ["option"]
    },
    "cni_bin_dir": "/opt/cni/bin",
    "cni_net_dir": "/etc/cni/net.d",
    "kubeconfig": "ZZZ-consul-cni-kubeconfig",
    "log_level": "info",
    "multus": false,
    "name": "consul-cni",
    "type": "consul-cni"
}`

const missingIPsStdinData = `{
    "cniVersion": "0.3.1",
	"name": "kindnet",
	"type": "kindnet",
    "capabilities": {
        "testCapability": false
    },
    "ipam": {
        "type": "host-local"
    },
    "dns": {
        "nameservers": ["nameserver"],
        "domain": "domain",
        "search": ["search"],
        "options": ["option"]
    },
    "prevResult": {
        "cniversion": "0.3.1",
        "interfaces": [
            {
                "name": "eth0",
                "sandbox": "/tmp"
            }
        ],
        "routes": []

    },
    "cni_bin_dir": "/opt/cni/bin",
    "cni_net_dir": "/etc/cni/net.d",
    "kubeconfig": "ZZZ-consul-cni-kubeconfig",
    "log_level": "info",
    "multus": false,
    "name": "consul-cni",
    "type": "consul-cni"
}`
