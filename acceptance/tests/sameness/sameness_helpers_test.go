// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package sameness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
