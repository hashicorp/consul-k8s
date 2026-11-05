// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package sameness

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	apicommon "github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSamenessSupportedPlatforms(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      config.TestConfig
		clusters int
		want     bool
	}{
		{name: "Kind", cfg: config.TestConfig{UseKind: true, EnableMultiCluster: true}, clusters: 4, want: true},
		{name: "enable OpenShift", cfg: config.TestConfig{EnableOpenshift: true, EnableMultiCluster: true}, clusters: 4, want: true},
		{name: "use OpenShift", cfg: config.TestConfig{UseOpenshift: true, EnableMultiCluster: true}, clusters: 4, want: true},
		{name: "more OpenShift contexts", cfg: config.TestConfig{UseOpenshift: true, EnableMultiCluster: true}, clusters: 5, want: true},
		{name: "unsupported platform", cfg: config.TestConfig{UseGKE: true, EnableMultiCluster: true}, clusters: 4},
		{name: "multi-cluster disabled", cfg: config.TestConfig{UseOpenshift: true}, clusters: 4},
		{name: "two OpenShift clusters", cfg: config.TestConfig{UseOpenshift: true, EnableMultiCluster: true}, clusters: 2},
		{name: "two Kind clusters", cfg: config.TestConfig{UseKind: true, EnableMultiCluster: true}, clusters: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.KubeEnvs = make([]config.KubeTestConfig, tc.clusters)
			require.Equal(t, tc.want, samenessTestConfigSupported(&tc.cfg))
		})
	}
}

func TestSamenessGroupAdmissionNamespace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		partition string
	}{
		{name: keyCluster01a, partition: "default"},
		{name: keyCluster01b, partition: cluster01Partition},
		{name: keyCluster02a, partition: "default"},
		{name: keyCluster03a, partition: "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, err := os.ReadFile("../fixtures/bases/sameness/" + tc.name + "-default-ns/sameness.yaml")
			require.NoError(t, err)
			var group v1alpha1.SamenessGroup
			require.NoError(t, yaml.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096).Decode(&group))
			meta := apicommon.ConsulMeta{Partition: tc.partition, DestinationNamespace: "default"}
			group.Namespace = "consul"
			require.ErrorContains(t, group.Validate(meta), "sameness groups must reside in the default namespace")
			group.Namespace = metav1.NamespaceDefault
			require.NoError(t, group.Validate(meta))
		})
	}
}

func TestSamenessGroupApplyAndCleanupNamespace(t *testing.T) {
	directory := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SAMENESS_KUBECTL_CALLS\"\ncase \"$*\" in *' get crd '*) printf '{\"items\":[]}\\n' ;; esac\n"
	require.NoError(t, os.WriteFile(filepath.Join(directory, "kubectl"), []byte(script), 0700))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		name      string
		namespace string
		cfg       config.TestConfig
	}{
		{name: "Kind", namespace: "default", cfg: config.TestConfig{UseKind: true}},
		{name: "OpenShift", namespace: "consul", cfg: config.TestConfig{EnableOpenshift: true}},
		{name: "OpenShift alternate namespace", namespace: "consul-secondary", cfg: config.TestConfig{UseOpenshift: true}},
	} {
		calls := filepath.Join(directory, tc.name+".calls")
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SAMENESS_KUBECTL_CALLS", calls)
			t.Cleanup(func() {
				require.Eventually(t, func() bool {
					output, err := os.ReadFile(calls)
					return err == nil && strings.Contains(string(output), " get crd ")
				}, 5*time.Second, 10*time.Millisecond, "wait for the cleanup helper's CRD discovery")
			})
			options := &terratestk8s.KubectlOptions{
				ContextName: "review-context", ConfigPath: "/review/kubeconfig", Namespace: tc.namespace,
			}
			c := &cluster{
				name: keyCluster01a, context: &samenessTestContext{options: options},
			}
			c.applySamenessGroup(t, &tc.cfg)
			require.Equal(t, tc.namespace, options.Namespace, "the installation namespace must not change")
		})
		output, err := os.ReadFile(calls)
		require.NoError(t, err)
		var commands []string
		for _, command := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if !strings.Contains(command, " get crd ") {
				commands = append(commands, command)
			}
		}
		require.Contains(t, commands[0], "apply")
		require.Contains(t, commands[len(commands)-1], "delete")
		for _, command := range commands {
			require.Contains(t, command, "--context review-context")
			require.Contains(t, command, "--namespace default")
		}
		if tc.cfg.EnableOpenshift || tc.cfg.UseOpenshift {
			require.Len(t, commands, 3)
			require.Contains(t, commands[1], "label samenessgroups.consul.hashicorp.com "+samenessGroupName)
			require.Contains(t, commands[1], "consul.hashicorp.com/acceptance-namespace="+tc.namespace)
		} else {
			require.Len(t, commands, 2)
		}
	}
}

func TestSamenessOpenShiftWorkloadLocality(t *testing.T) {
	for _, cfg := range []*config.TestConfig{{EnableOpenshift: true}, {UseOpenshift: true}} {
		client := kubernetesfake.NewClientset(
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "first-node", Labels: map[string]string{corev1.LabelTopologyRegion: "wrong-region"},
			}},
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "worker", Labels: map[string]string{
					corev1.LabelTopologyRegion: "actual-region",
					corev1.LabelTopologyZone:   "actual-zone",
				},
			}},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "server", Namespace: staticServerNamespace, Labels: map[string]string{"app": staticServerName}},
				Spec:       corev1.PodSpec{NodeName: "worker"},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "other", Labels: map[string]string{"app": staticServerName}},
				Spec:       corev1.PodSpec{NodeName: "first-node"},
			},
		)
		context := &samenessTestContext{kubernetes: client}
		c := &cluster{context: context, locality: localityForRegion("synthetic")}
		setK8sNodeLocality(t, cfg, context, c)
		require.Empty(t, client.Actions(), "OpenShift must not relabel managed nodes")
		require.Equal(t, api.Locality{Region: "actual-region", Zone: "actual-zone"},
			c.workloadLocality(t, staticServerNamespace, staticServerName))
		node, err := client.CoreV1().Nodes().Get(t.Context(), "worker", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "actual-region", node.Labels[corev1.LabelTopologyRegion])
		require.Equal(t, "actual-zone", node.Labels[corev1.LabelTopologyZone])
	}
}

func TestSamenessOpenShiftPodScope(t *testing.T) {
	pod := func(name, namespace, app string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"app": app}},
			Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: app}, {Name: "envoy"},
			}},
			Status: corev1.PodStatus{PodIP: "10.0.0.1"},
		}
	}
	kubernetes := kubernetesfake.NewClientset(
		pod("server", staticServerNamespace, staticServerName),
		pod("client", staticClientNamespace, staticClientName),
		pod("unrelated-server", "other", staticServerName),
		pod("unrelated-client", "other", staticClientName),
	)
	c := &cluster{
		context:    &samenessTestContext{kubernetes: kubernetes},
		serverOpts: &terratestk8s.KubectlOptions{Namespace: staticServerNamespace},
		clientOpts: &terratestk8s.KubectlOptions{Namespace: staticClientNamespace},
	}
	clusters{"test": c}.setServerIP(t, &config.TestConfig{UseOpenshift: true})
	require.NotNil(t, c.staticServerIP)
	require.Equal(t, "10.0.0.1", *c.staticServerIP)
}

func TestSamenessDNSQueryNamespace(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "kubectl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, namespace := range []string{"default", "consul"} {
		t.Run(namespace, func(t *testing.T) {
			c := &cluster{
				name: "test",
				context: &samenessTestContext{options: &terratestk8s.KubectlOptions{
					ContextName: "review-context", ConfigPath: "/review/kubeconfig", Namespace: namespace,
				}},
				clientOpts: &terratestk8s.KubectlOptions{
					ContextName: "review-context", ConfigPath: "/review/kubeconfig", Namespace: staticClientNamespace,
				},
			}
			output := dnsQuery(t, &config.TestConfig{}, "test-release", []string{"service.consul"}, c, c)
			require.Contains(t, strings.Split(output, "\n"), "@test-release-consul-dns."+namespace)
		})
	}
}

func TestSamenessDNSFailoverIdentity(t *testing.T) {
	c := &cluster{
		name:    peerName2a,
		context: &samenessTestContext{options: &terratestk8s.KubectlOptions{ContextName: "kind-dc2"}},
	}
	require.Equal(t, "dc2", c.dnsFailoverName(t, &config.TestConfig{UseKind: true}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/catalog/service/static-server" ||
			r.URL.Query().Get("ns") != staticServerNamespace ||
			r.URL.Query().Get("partition") != cluster01Partition {
			t.Errorf("unexpected catalog request: %s", r.URL.RequestURI())
		}
		if err := json.NewEncoder(w).Encode([]*api.CatalogService{{
			Node: "actual-worker-virtual", ServiceName: staticServerName,
		}}); err != nil {
			t.Errorf("writing catalog response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	var err error
	c.client, err = api.NewClient(&api.Config{Address: server.URL})
	require.NoError(t, err)
	c.partition = cluster01Partition
	c.context = &samenessTestContext{options: &terratestk8s.KubectlOptions{ContextName: "default/api.openshift.example:443/user"}}
	require.Equal(t, "actual-worker-virtual", c.dnsFailoverName(t, &config.TestConfig{EnableOpenshift: true}))
	require.Equal(t, "actual-worker-virtual", c.dnsFailoverName(t, &config.TestConfig{UseOpenshift: true}))
}

type samenessTestContext struct {
	options    *terratestk8s.KubectlOptions
	kubernetes kubernetes.Interface
}

func (c *samenessTestContext) KubectlOptions(t testutil.TestingTB) *terratestk8s.KubectlOptions {
	require.NotNil(t, c.options, "unexpected kubectl options request")
	return c.options
}

func (c *samenessTestContext) KubectlOptionsForNamespace(namespace string) *terratestk8s.KubectlOptions {
	options := *c.options
	options.Namespace = namespace
	return &options
}

func (c *samenessTestContext) KubernetesClient(t testutil.TestingTB) kubernetes.Interface {
	require.NotNil(t, c.kubernetes, "unexpected Kubernetes client request")
	return c.kubernetes
}

func (c *samenessTestContext) ControllerRuntimeClient(t testutil.TestingTB) client.Client {
	t.Fatalf("unexpected controller-runtime client request")
	return nil
}
