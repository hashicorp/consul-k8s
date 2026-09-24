// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"os"
	"path/filepath"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	runtimefake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
)

// standardCorefile is the Corefile an unmanaged CoreDNS ships with: no imports,
// so the test owns the file and may overwrite it.
const standardCorefile = `.:53 {
    errors
    health
    kubernetes cluster.local in-addr.arpa ip6.arpa {
       pods insecure
    }
    forward . /etc/resolv.conf
    cache 30
}
`

// managedCorefile is the shape AKS ships: the Corefile is owned by the addon
// reconciler and pulls extra server blocks in from the coredns-custom
// ConfigMap, which is the only place a stub domain survives.
const managedCorefile = `.:53 {
    errors
    health
    kubernetes cluster.local in-addr.arpa ip6.arpa {
       pods insecure
    }
    forward . /etc/resolv.conf
    cache 30
    import custom/*.override
}
import custom/*.server
`

// TestCoreDNSConfigMapToPatch pins which ConfigMap each platform's CoreDNS gets
// patched through. Picking coredns-custom on a cluster whose Corefile is not
// managed would write a stub domain nothing reads, and picking the Corefile on
// a managed cluster would write one the reconciler throws away, so this is the
// decision the whole DNS suite rests on.
func TestCoreDNSConfigMapToPatch(t *testing.T) {
	tests := []struct {
		name       string
		configMaps []*corev1.ConfigMap
		want       string
	}{
		{
			name: "GKE keeps kube-dns, which has no Corefile to inspect",
			configMaps: []*corev1.ConfigMap{
				dnsConfigMap("kube-dns", map[string]string{"k8s-app": "kube-dns"},
					map[string]string{"stubDomains": `{"consul": ["10.0.0.1"]}`}),
			},
			want: "kube-dns",
		},
		{
			name: "EKS and kind keep the Corefile strategy",
			configMaps: []*corev1.ConfigMap{
				dnsConfigMap("coredns", map[string]string{"k8s-app": "kube-dns"},
					map[string]string{"Corefile": standardCorefile}),
			},
			want: "coredns",
		},
		{
			name: "a managed Corefile is patched through coredns-custom",
			configMaps: []*corev1.ConfigMap{
				dnsConfigMap("coredns", map[string]string{"k8s-app": "kube-dns"},
					map[string]string{"Corefile": managedCorefile}),
			},
			want: coreDNSCustomConfigMap,
		},
		{
			name: "the import is recognised however its path is spelled",
			configMaps: []*corev1.ConfigMap{
				dnsConfigMap("coredns", map[string]string{"k8s-app": "kube-dns"},
					map[string]string{"Corefile": ".:53 {\n    cache 30\n}\nimport /etc/coredns/custom/*.server\n"}),
			},
			want: coreDNSCustomConfigMap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0, len(tt.configMaps))
			for _, cm := range tt.configMaps {
				objs = append(objs, cm)
			}
			ctx := &fakeDNSContext{client: fake.NewSimpleClientset(objs...)}
			require.Equal(t, tt.want, coreDNSConfigMapToPatch(t, ctx))
		})
	}
}

func dnsConfigMap(name string, labels, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system", Labels: labels},
		Data:       data,
	}
}

type fakeDNSContext struct {
	client kubernetes.Interface
}

func (c *fakeDNSContext) Name() string { return "fake" }
func (c *fakeDNSContext) KubectlOptions(_ testutil.TestingTB) *terratestk8s.KubectlOptions {
	return &terratestk8s.KubectlOptions{Namespace: "default"}
}
func (c *fakeDNSContext) KubectlOptionsForNamespace(ns string) *terratestk8s.KubectlOptions {
	return &terratestk8s.KubectlOptions{Namespace: ns}
}
func (c *fakeDNSContext) KubernetesClient(_ testutil.TestingTB) kubernetes.Interface {
	return c.client
}
func (c *fakeDNSContext) ControllerRuntimeClient(_ testutil.TestingTB) runtimeclient.Client {
	return runtimefake.NewClientBuilder().Build()
}

var _ environment.TestContext = (*fakeDNSContext)(nil)

// TestUpdateCoreDNSFileByPlatform runs the manifest generation for each
// platform and checks what actually lands on disk. The strategy choice is only
// half the story: the generated ConfigMap has to be the shape that platform's
// CoreDNS reads, and GKE's and EKS's output must not have drifted while the
// managed-CoreDNS path was added.
func TestUpdateCoreDNSFileByPlatform(t *testing.T) {
	const releaseName = "test-release"

	tests := []struct {
		name           string
		dnsConfigMap   *corev1.ConfigMap
		enableDNSProxy bool
		wantName       string
		wantKey        string
		wantContains   []string
	}{
		{
			name: "GKE writes stubDomains into kube-dns",
			dnsConfigMap: dnsConfigMap("kube-dns", map[string]string{"k8s-app": "kube-dns"},
				map[string]string{"stubDomains": "{}"}),
			enableDNSProxy: false,
			wantName:       "kube-dns",
			wantKey:        "stubDomains",
			wantContains:   []string{`{"consul": ["10.0.0.42"]}`},
		},
		{
			name: "EKS writes a consul server block into the Corefile",
			dnsConfigMap: dnsConfigMap("coredns", map[string]string{"k8s-app": "kube-dns"},
				map[string]string{"Corefile": standardCorefile}),
			enableDNSProxy: true,
			wantName:       "coredns",
			wantKey:        "Corefile",
			wantContains:   []string{"consul:53 {", "forward . 10.0.0.42:8600", "kubernetes cluster.local"},
		},
		{
			name: "a managed CoreDNS gets a coredns-custom server block",
			dnsConfigMap: dnsConfigMap("coredns", map[string]string{"k8s-app": "kube-dns"},
				map[string]string{"Corefile": managedCorefile}),
			enableDNSProxy: true,
			wantName:       coreDNSCustomConfigMap,
			wantKey:        "consul.server",
			wantContains:   []string{"consul:53 {", "forward . 10.0.0.42:8600"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dnsSvc := releaseName + "-consul-dns"
			if tt.enableDNSProxy {
				dnsSvc += "-proxy"
			}
			ctx := &fakeDNSContext{client: fake.NewSimpleClientset(
				tt.dnsConfigMap,
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: dnsSvc, Namespace: "default"},
					Spec:       corev1.ServiceSpec{ClusterIP: "10.0.0.42"},
				},
			)}

			target := coreDNSConfigMapToPatch(t, ctx)
			require.Equal(t, tt.wantName, target, "strategy picked the wrong ConfigMap")

			out := filepath.Join(t.TempDir(), "generated.yaml")
			updateCoreDNSFile(t, ctx, releaseName, tt.enableDNSProxy, "8600", out, target)

			raw, err := os.ReadFile(out)
			require.NoError(t, err)

			// Parsing proves the manifest is well formed, not just that the
			// text looks right.
			var generated struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name      string `yaml:"name"`
					Namespace string `yaml:"namespace"`
				} `yaml:"metadata"`
				Data map[string]string `yaml:"data"`
			}
			require.NoError(t, yaml.Unmarshal(raw, &generated), "generated manifest is not valid YAML:\n%s", raw)
			require.Equal(t, "ConfigMap", generated.Kind)
			require.Equal(t, tt.wantName, generated.Metadata.Name)
			require.Equal(t, "kube-system", generated.Metadata.Namespace)
			require.Contains(t, generated.Data, tt.wantKey, "generated manifest is missing the key CoreDNS reads")

			for _, want := range tt.wantContains {
				require.Contains(t, generated.Data[tt.wantKey], want)
			}

			// The managed path must never touch the Corefile, which is the
			// whole reason it exists.
			if tt.wantName == coreDNSCustomConfigMap {
				require.NotContains(t, generated.Data, "Corefile",
					"a managed CoreDNS must not have its Corefile rewritten")
			}
		})
	}
}
