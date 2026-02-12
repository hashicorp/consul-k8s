// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/hashicorp/go-hclog"
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
		cmdArgs       *skel.CmdArgs
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
				pod.Annotations[annotationDualStack] = "consul.hashicorp.com/dual-stack"
				_, err = cmd.client.CoreV1().Pods(defaultNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				return pod
			},
			expectedErr:   nil,
			expectedRules: true, // Rules will be applied
		},
		{
			name: "Pod with correct annotations, using projected tokens should create redirect traffic rules",
			cmd: &Command{
				client:           fake.NewSimpleClientset(),
				iptablesProvider: &fakeIptablesProvider{},
			},
			podName:   "pod-no-proxy-outbound-port",
			stdInData: goodStdinDataWithProjectedToken,
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
				pod.Annotations[annotationDualStack] = "consul.hashicorp.com/dual-stack"
				_, err = cmd.client.CoreV1().Pods(defaultNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, err)

				return pod
			},
			expectedErr:   nil,
			expectedRules: true, // Rules will be applied
		},
		{
			name: "Parsing iptables from CNI_ARGs as in Nomad",
			cmd: &Command{
				client:           fake.NewSimpleClientset(),
				iptablesProvider: &fakeIptablesProvider{},
			},
			cmdArgs: &skel.CmdArgs{ContainerID: "some-container-id",
				IfName: "eth0",
				Args:   fmt.Sprintf("CONSUL_IPTABLES_CONFIG=%s", minimalIPTablesJSON(t)),
				Path:   "/some/bin/path",
			},
			stdInData:     nomadStdinData,
			expectedErr:   nil,
			expectedRules: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.cmdArgs != nil {
				c.cmdArgs.StdinData = []byte(c.stdInData)
				err := c.cmd.cmdAdd(c.cmdArgs)
				require.Equal(t, c.expectedErr, err)
			} else {
				_ = c.configuredPod(minimalPod(c.podName), c.cmd)
				err := c.cmd.cmdAdd(minimalSkelArgs(c.podName, defaultNamespace, c.stdInData))
				require.Equal(t, c.expectedErr, err)
			}

			// Check to see that rules have been generated
			if c.expectedErr == nil && c.expectedRules {
				require.NotEmpty(t, c.cmd.iptablesProvider.Rules())
			}
		})
	}
}

func writeKubeconfig(t *testing.T, dir string, valid bool) string {
	t.Helper()

	var data string
	if valid {
		data = `
apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: fake-token
`
	} else {
		data = `
apiVersion: v1
kind: Config
clusters:
- name: test
  cluster: {}
`
	}

	path := filepath.Join(dir, "kubeconfig")

	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}

	return path
}

// TestResolveKubeconfigPath tests the resolveKubeconfigPath function
func TestResolveKubeconfigPath(t *testing.T) {
	tests := []struct {
		name                  string
		setup                 func(t *testing.T, dir string)
		wantSuffix            string
		expectError           bool
		expectedErrorContains error
	}{
		{
			name: "stable kubeconfig exists",
			setup: func(t *testing.T, dir string) {
				path := filepath.Join(dir, "kubeconfig")
				if err := os.WriteFile(path, []byte("stable"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantSuffix:  "kubeconfig",
			expectError: false,
		},
		{
			name: "stable path is directory, fallback to versioned",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "kubeconfig"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(
					filepath.Join(dir, "kubeconfig-1"),
					[]byte("v1"),
					0644,
				); err != nil {
					t.Fatal(err)
				}
			},
			wantSuffix:  "kubeconfig-1",
			expectError: false,
		},
		{
			name: "single versioned kubeconfig",
			setup: func(t *testing.T, dir string) {
				if err := os.WriteFile(
					filepath.Join(dir, "kubeconfig-123"),
					[]byte("v123"),
					0644,
				); err != nil {
					t.Fatal(err)
				}
			},
			wantSuffix:  "kubeconfig-123",
			expectError: false,
		},
		{
			name: "multiple versioned kubeconfigs, newest chosen",
			setup: func(t *testing.T, dir string) {
				old := filepath.Join(dir, "kubeconfig-old")
				newer := filepath.Join(dir, "kubeconfig-new")

				if err := os.WriteFile(old, []byte("old"), 0644); err != nil {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond) // ensure mtime difference
				if err := os.WriteFile(newer, []byte("new"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantSuffix:  "kubeconfig-new",
			expectError: false,
		},
		{
			name: "no kubeconfig files",
			setup: func(t *testing.T, dir string) {
				// nothing created
			},
			expectError:           true,
			expectedErrorContains: fmt.Errorf("no kubeconfig found"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)

			got, err := resolveKubeconfigPath(dir, "kubeconfig")

			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil (path=%s)", got)
					return
				}
				require.Contains(t, err.Error(), tc.expectedErrorContains.Error())
				return

			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !filepath.IsAbs(got) {
				t.Fatalf("expected absolute path, got %s", got)
			}

			if !strings.HasSuffix(got, tc.wantSuffix) {
				t.Fatalf("expected suffix %q, got %q", tc.wantSuffix, got)
			}
		})
	}
}

func TestCreateK8sClient(t *testing.T) {
	t.Parallel()
	logger := hclog.NewNullLogger()

	tests := []struct {
		setup                 func(t *testing.T) *PluginConf
		expectedErrorContains error
		expectedError         bool
		expectClient          bool
		name                  string
	}{
		{
			name: "Client success",
			setup: func(t *testing.T) *PluginConf {
				dir := t.TempDir()
				writeKubeconfig(t, dir, true)
				return &PluginConf{
					CNINetDir:  dir,
					Kubeconfig: "kubeconfig",
				}
			},
			expectedError: false,
			expectClient:  true,
		},
		{
			name: "No Kubeconfig found",
			setup: func(t *testing.T) *PluginConf {
				dir := t.TempDir()
				return &PluginConf{
					CNINetDir:  dir,
					Kubeconfig: "",
				}
			},
			expectedErrorContains: fmt.Errorf("failed to load kubeconfig"),
			expectedError:         true,
			expectClient:          false,
		},
		{
			name: "error from BuildConfigFromFlags",
			setup: func(t *testing.T) *PluginConf {
				dir := t.TempDir()
				writeKubeconfig(t, dir, false) // invalid content
				return &PluginConf{
					CNINetDir:  dir,
					Kubeconfig: "kubeconfig", // ALWAYS this
				}
			},
			expectedErrorContains: fmt.Errorf("failed to load kubeconfig"),
			expectedError:         true,
			expectClient:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setup(t)
			cmd := &Command{}
			err := cmd.createK8sClient(cfg, logger)
			if tt.expectedError {
				require.Contains(t, err.Error(), tt.expectedErrorContains.Error())
				t.Logf("✅ expected error occurred: %v", err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cmd.client)
				t.Log("✅ client created successfully")
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
			name: "Pod with v2 annotations correctly set",
			annotatedPod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[keyMeshInjectStatus] = "foo"
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
	"cni_host_token_path":"",
	"cni_token_path": "/var/run/secrets/kubernetes.io/serviceaccount/token",
	"autorotate_token": false,
    "multus": false,
    "name": "consul-cni",
    "type": "consul-cni"
}`

const goodStdinDataWithProjectedToken = `{
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
	"cni_token_path": "/var/run/secrets/kubernetes.io/serviceaccount/token",
	"cni_host_token_path": "/etc/cni/net.d/cni-host-token",
	"autorotate_token": true,
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
	"cni_host_token_path":"",
	"cni_token_path": "/var/run/secrets/kubernetes.io/serviceaccount/token",
	"autorotate_token": false,
    "multus": false,
    "name": "consul-cni",
    "type": "consul-cni"
}`

const nomadStdinData = `{
    "cniVersion": "0.4.0",
    "dns": {},
    "prevResult": {
        "cniversion": "0.4.0",
        "interfaces": [
            {
                "name": "eth0",
                "mac": "aa:bb:cc:dd:ee:ff",
                "sandbox": "/var/rum/netns/16c"
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
    "log_level": "info",
	"cni_host_token_path":"",
	"cni_token_path": "/var/run/secrets/kubernetes.io/serviceaccount/token",
	"autorotate_token": false,
    "name": "nomad",
    "type": "consul-cni"
}
`

func minimalIPTablesJSON(t *testing.T) string {
	cfg := iptables.Config{
		ConsulDNSIP:          "127.0.0.1",
		ConsulDNSPort:        8600,
		ProxyUserID:          "101",
		ProxyInboundPort:     20000,
		ProxyOutboundPort:    15001,
		ExcludeInboundPorts:  []string{"9000"},
		ExcludeOutboundPorts: []string{"15002"},
		ExcludeOutboundCIDRs: []string{"10.0.0.0/24"},
		ExcludeUIDs:          []string{"1", "42"},
		NetNS:                "/some/netns/path",
	}
	buf, err := json.Marshal(cfg)
	require.NoError(t, err)
	return string(buf)
}
