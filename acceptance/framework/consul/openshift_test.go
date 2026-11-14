// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	customgwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	kubeversion "k8s.io/apimachinery/pkg/version"
	discoveryfake "k8s.io/client-go/discovery/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestOpenShiftReleaseStorageTimeout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		want    bool
	}{
		{name: "observed API timeout", message: "create: failed to create: the server was unable to return a response in the time allotted, but may still be processing the request (post secrets)", want: true},
		{name: "etcd timeout", message: "create: failed to create: etcdserver: request timed out (post secrets)", want: true},
		{name: "unhealthy workload", message: "timed out waiting for the condition"},
		{name: "invalid license", message: "invalid Consul enterprise license"},
		{name: "authentication", message: "create: failed to create: Unauthorized (post secrets)"},
		{name: "ownership conflict", message: "CustomResourceDefinition exists and cannot be imported into the current release"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isOpenShiftReleaseStorageTimeout(tc.message))
		})
	}
}

func TestOpenShiftStorageRetryBudget(t *testing.T) {
	directory := t.TempDir()
	calls := filepath.Join(directory, "helm-calls")
	t.Setenv("OCP_HELM_CALLS", calls)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\necho \"$*\" >> \"$OCP_HELM_CALLS\"\necho 'release: not found' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(directory, "helm"), []byte(script), 0700))
	attempts := 0
	err := installOpenShiftWithStorageRetry(t, &helm.Options{}, "test-release", func() error {
		attempts++
		return errors.New("create: failed to create: the server was unable to return a response in the time allotted (post secrets)")
	})
	require.Error(t, err)
	require.Equal(t, 3, attempts)
	output, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(output), "uninstall test-release --no-hooks --timeout 2m"))
}

func TestCleanupOpenShiftCustomGatewayClasses(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, customgwv1beta1.Install(scheme))
	newClass := func(name, release, namespace string) *customgwv1beta1.CustomGatewayClass {
		return &customgwv1beta1.CustomGatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					"release": release,
					"consul.hashicorp.com/acceptance-namespace": namespace,
				},
				Finalizers: []string{"gateway-exists-finalizer.consul.hashicorp.com"},
			},
		}
	}
	kubernetesClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newClass("managed", "test-release", ""),
		newClass("fixture", "", "consul"),
		newClass("other-release", "another-release", "consul"),
		newClass("other-namespace", "", "another-namespace"),
	).Build()
	cluster := &HelmCluster{
		releaseName:   "test-release",
		runtimeClient: kubernetesClient,
		helmOptions:   &helm.Options{KubectlOptions: &terratestk8s.KubectlOptions{Namespace: "consul"}},
	}
	require.NoError(t, cluster.cleanupOpenShiftCustomGatewayClasses(t))
	var classes customgwv1beta1.CustomGatewayClassList
	require.NoError(t, kubernetesClient.List(context.Background(), &classes))
	require.Len(t, classes.Items, 2)
	for _, gatewayClass := range classes.Items {
		require.Contains(t, []string{"other-release", "other-namespace"}, gatewayClass.Name)
		require.NotEmpty(t, gatewayClass.Finalizers)
		require.NoError(t, kubernetesClient.Get(context.Background(), client.ObjectKey{Name: gatewayClass.Name}, &gatewayClass))
	}
}

func TestOpenShiftCleanupRetriesDiscoveryTimeout(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, customgwv1beta1.Install(scheme))
	calls := 0
	kubernetesClient := clientfake.NewClientBuilder().WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, underlying client.WithWatch, list client.ObjectList, options ...client.ListOption) error {
				calls++
				if calls == 1 {
					return fmt.Errorf("failed to get server groups: %w", syscall.ETIMEDOUT)
				}
				return underlying.List(ctx, list, options...)
			},
		}).Build()
	cluster := &HelmCluster{
		runtimeClient: kubernetesClient,
		helmOptions:   &helm.Options{KubectlOptions: &terratestk8s.KubectlOptions{Namespace: "consul"}},
	}
	require.NoError(t, cluster.cleanupOpenShiftCustomGatewayClasses(t))
	require.Equal(t, 2, calls)
}

func TestOpenShiftAPIOperationRequestDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, runOpenShiftAPIOperation(ctx, func(requestContext context.Context) error {
		deadline, ok := requestContext.Deadline()
		require.True(t, ok)
		require.InDelta(t, 30, time.Until(deadline).Seconds(), 1)
		return nil
	}))
}

func TestOpenShiftUninstallAbsentRelease(t *testing.T) {
	directory := t.TempDir()
	calls := filepath.Join(directory, "calls")
	t.Setenv("OCP_HELM_CALLS", calls)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\necho \"$*\" >> \"$OCP_HELM_CALLS\"\necho 'Error: uninstall: Release not loaded: test-never-installed: release: not found' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(directory, "helm"), []byte(script), 0700))
	require.NoError(t, uninstallOpenShiftRelease(t, &helm.Options{}, "test-never-installed"))
	output, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(output), "uninstall test-never-installed"))
	require.False(t, isOpenShiftReleaseNotFound("test-never-installed", "Error: uninstall: failed to get server groups", errors.New("unauthorized")))
}

func TestOpenShiftOrphanedCRDRecovery(t *testing.T) {
	const oldRelease = "test-strfhh"
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	for _, tc := range []struct {
		name             string
		authorized       bool
		activePod        bool
		foreignCRD       bool
		foreignInstance  bool
		foreignFinalizer bool
		wantRemoved      bool
		wantError        string
	}{
		{name: "observed orphaned gateway CRD", authorized: true, wantRemoved: true},
		{name: "unmarked namespace"},
		{name: "live Consul installation", authorized: true, activePod: true},
		{name: "foreign CRD ownership", authorized: true, foreignCRD: true},
		{name: "foreign instance", authorized: true, foreignInstance: true, wantError: "no acceptance ownership"},
		{name: "foreign finalizer", authorized: true, foreignFinalizer: true, wantError: "foreign finalizer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "consul"}}
			if tc.authorized {
				namespace.Labels = map[string]string{"consul.hashicorp.com/acceptance-tests": "true"}
			}
			crd := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "gatewayclassconfigs.consul.hashicorp.com",
					Labels: map[string]string{"release": oldRelease},
					Annotations: map[string]string{
						"meta.helm.sh/release-name": oldRelease, "meta.helm.sh/release-namespace": "consul",
					},
				},
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Group: "consul.hashicorp.com",
					Names: apiextensionsv1.CustomResourceDefinitionNames{
						Kind: "GatewayClassConfig", ListKind: "GatewayClassConfigList", Plural: "gatewayclassconfigs",
					},
					Scope:    apiextensionsv1.ClusterScoped,
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1alpha1", Served: true, Storage: true}},
				},
			}
			if tc.foreignCRD {
				crd.Annotations["meta.helm.sh/release-namespace"] = "another-namespace"
			}
			gatewayConfig := &v1alpha1.GatewayClassConfig{ObjectMeta: metav1.ObjectMeta{
				Name: "consul-api-gateway", Labels: map[string]string{"release": oldRelease},
				Finalizers: []string{"gateway-class-exists-finalizer.consul.hashicorp.com"},
			}}
			if tc.foreignInstance {
				gatewayConfig.Labels = nil
			}
			if tc.foreignFinalizer {
				gatewayConfig.Finalizers = append(gatewayConfig.Finalizers, "another-controller.example/finalizer")
			}
			objects := []client.Object{namespace, crd, gatewayConfig}
			if tc.activePod {
				objects = append(objects, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name: "production-server", Namespace: "another-namespace", Labels: map[string]string{"app": "consul"},
				}})
			}
			kubernetesClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			cluster := &HelmCluster{
				runtimeClient: kubernetesClient,
				helmOptions:   &helm.Options{KubectlOptions: &terratestk8s.KubectlOptions{Namespace: "consul"}},
			}
			err := cluster.cleanupOpenShiftOrphanedCRDs(t)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			crdError := kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(crd), crd)
			if tc.wantRemoved {
				require.True(t, apierrors.IsNotFound(crdError))
				require.True(t, apierrors.IsNotFound(kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(gatewayConfig), gatewayConfig)))
			} else {
				require.NoError(t, crdError)
				require.NoError(t, kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(gatewayConfig), gatewayConfig))
				require.NotEmpty(t, gatewayConfig.Finalizers)
			}
		})
	}
}

func TestOpenShiftGatewayCRDOwnership(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is required to validate local chart rendering")
	}
	for _, custom := range []bool{false, true} {
		name := "standard"
		if custom {
			name = "Consul-scoped"
		}
		t.Run(name, func(t *testing.T) {
			client := kubernetesfake.NewClientset()
			client.Discovery().(*discoveryfake.FakeDiscovery).FakedServerVersion = &kubeversion.Info{GitVersion: "v1.35.8"}
			values := openShiftHelmValues(t, client)
			helpers.MergeMaps(values, map[string]string{"connectInject.enabled": "true"})
			if custom {
				values["global.openshift.crds.consulapi.enabled"] = "true"
			}
			args := []string{"template", "test-ocp", config.HelmChartPath}
			for key, value := range values {
				args = append(args, "--set", key+"="+value)
			}
			output, err := exec.Command(helm, args...).CombinedOutput()
			require.NoError(t, err, string(output))
			decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(output)), 4096)
			servedVersions := map[string][]string{}
			for {
				var manifest struct {
					Kind     string `json:"kind"`
					Metadata struct {
						Name string `json:"name"`
					} `json:"metadata"`
					Spec struct {
						Group    string `json:"group"`
						Versions []struct {
							Name   string `json:"name"`
							Served bool   `json:"served"`
						} `json:"versions"`
					} `json:"spec"`
				}
				err := decoder.Decode(&manifest)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				if manifest.Kind != "CustomResourceDefinition" {
					continue
				}
				require.NotEqual(t, "gateway.networking.k8s.io", manifest.Spec.Group, "OCP-owned Gateway API CRDs must not be rendered")
				for _, version := range manifest.Spec.Versions {
					if version.Served {
						servedVersions[manifest.Metadata.Name] = append(servedVersions[manifest.Metadata.Name], version.Name)
					}
				}
			}
			require.Contains(t, servedVersions, "gatewayclassconfigs.consul.hashicorp.com")
			if custom {
				require.Contains(t, servedVersions["cgateways.consul.hashicorp.com"], "v1beta1")
				require.Contains(t, servedVersions["chttproutes.consul.hashicorp.com"], "v1beta1")
				require.Contains(t, servedVersions["ctcproutes.consul.hashicorp.com"], "v1alpha2")
				require.Contains(t, servedVersions, "customgatewayclasses.consul.hashicorp.com")
			}
		})
	}
}

func TestOpenShiftHelmValues(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    string
		ocp419Plus string
		tcpRoutes  string
	}{
		{name: "OCP 4.18", version: "v1.31.9", ocp419Plus: "false", tcpRoutes: "true"},
		{name: "OCP 4.19", version: "v1.32.1+abc123", ocp419Plus: "true", tcpRoutes: "false"},
		{name: "OCP 4.22", version: "v1.35.0", ocp419Plus: "true", tcpRoutes: "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewClientset()
			client.Discovery().(*discoveryfake.FakeDiscovery).FakedServerVersion = &kubeversion.Info{GitVersion: tc.version}
			values := openShiftHelmValues(t, client)
			require.Equal(t, tc.ocp419Plus, values["global.openshift.isOcpGreaterthan4_18"])
			require.Equal(t, tc.tcpRoutes, values["global.openshift.crds.enableTcpRoute"])
			require.Equal(t, "false", values["connectInject.apiGateway.manageExternalCRDs"])
			require.Equal(t, "true", values["connectInject.apiGateway.manageNonStandardCRDs"])
			require.NotContains(t, values, "server.containerSecurityContext.server.runAsNonRoot")
		})
	}
}

func TestQualifyReleasedChartImages(t *testing.T) {
	released := `
global:
  image: hashicorp/consul:2.0.3
  imageK8S: hashicorp/consul-k8s-control-plane:2.0.3
  imageEnvoy: null
  imageConsulDataplane: hashicorp/consul-dataplane:2.0.3
server:
  image: null
`
	for _, tc := range []struct {
		name        string
		chartValues string
		setValues   map[string]string
		want        map[string]string
	}{
		{
			name:        "short names",
			chartValues: released,
			want: map[string]string{
				"global.image":                "docker.io/hashicorp/consul:2.0.3",
				"global.imageK8S":             "docker.io/hashicorp/consul-k8s-control-plane:2.0.3",
				"global.imageConsulDataplane": "docker.io/hashicorp/consul-dataplane:2.0.3",
			},
		},
		{
			name:        "enterprise image already set",
			chartValues: released,
			setValues:   map[string]string{"global.image": "hashicorp/consul-enterprise:2.0.3-ent"},
			want: map[string]string{
				"global.imageK8S":             "docker.io/hashicorp/consul-k8s-control-plane:2.0.3",
				"global.imageConsulDataplane": "docker.io/hashicorp/consul-dataplane:2.0.3",
			},
		},
		{
			name:        "qualified names",
			chartValues: "global:\n  image: docker.mirror.hashicorp.services/hashicorppreview/consul:1.22-dev\n  imageK8S: quay.io/hashicorp/consul-k8s-control-plane:2.0.3\n",
			want:        map[string]string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			images, err := qualifyReleasedChartImages([]byte(tc.chartValues), tc.setValues)
			require.NoError(t, err)
			require.Equal(t, tc.want, images)
		})
	}

	_, err := qualifyReleasedChartImages([]byte("global: ["), nil)
	require.Error(t, err)
}

func TestIsShortImageName(t *testing.T) {
	for image, want := range map[string]bool{
		"hashicorp/consul:2.0.3":                         true,
		"busybox":                                        true,
		"docker.io/hashicorp/consul:2.0.3":               false,
		"localhost/consul:dev":                           false,
		"registry:5000/consul:dev":                       false,
		"registry.connect.redhat.com/hashicorp/consul:1": false,
	} {
		require.Equal(t, want, isShortImageName(image), image)
	}
}

func TestCreateOrUpdateLicenseSecret(t *testing.T) {
	for _, tc := range []struct {
		name        string
		openShift   bool
		existing    string
		wantLicense string
	}{
		{name: "OpenShift creates missing license", openShift: true, wantLicense: "current-license"},
		{name: "OpenShift refreshes stale license", openShift: true, existing: "stale-license", wantLicense: "current-license"},
		{name: "OpenShift keeps current license", openShift: true, existing: "current-license", wantLicense: "current-license"},
		{name: "non-OpenShift creates missing license", wantLicense: "current-license"},
		{name: "non-OpenShift keeps existing license", existing: "stale-license", wantLicense: "stale-license"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "consul"}})
			if tc.existing != "" {
				_, err := client.CoreV1().Secrets("consul").Create(context.Background(), &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: config.LicenseSecretName},
					Data:       map[string][]byte{config.LicenseSecretKey: []byte(tc.existing)},
				}, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			cfg := &config.TestConfig{EnterpriseLicense: "current-license", NoCleanup: true, EnableOpenshift: tc.openShift}
			createOrUpdateLicenseSecret(t, client, cfg, "consul")
			secret, err := client.CoreV1().Secrets("consul").Get(context.Background(), config.LicenseSecretName, metav1.GetOptions{})
			require.NoError(t, err)
			if tc.existing == "" {
				require.Equal(t, tc.wantLicense, secret.StringData[config.LicenseSecretKey])
			} else {
				require.Equal(t, tc.wantLicense, string(secret.Data[config.LicenseSecretKey]))
			}
		})
	}
}

func TestEnsureNamespaceExists(t *testing.T) {
	client := kubernetesfake.NewClientset()
	ensureNamespaceExists(t, client, "consul")
	ensureNamespaceExists(t, client, "consul")
	namespaces, err := client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, namespaces.Items, 1)
	require.Equal(t, "consul", namespaces.Items[0].Name)
}

func TestIsReleaseSecret(t *testing.T) {
	for _, tc := range []struct {
		name        string
		secret      corev1.Secret
		wantOCP     bool
		wantDefault bool
	}{
		{
			name:        "TLS secret",
			secret:      corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "consul-ca-cert"}},
			wantOCP:     true,
			wantDefault: true,
		},
		{
			name:        "Helm release secret",
			secret:      corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "sh.helm.release.v1.consul.v1"}},
			wantOCP:     true,
			wantDefault: true,
		},
		{
			name:        "own generated dockercfg",
			secret:      corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "consul-tls-init-dockercfg-abcde"}},
			wantOCP:     true,
			wantDefault: true,
		},
		{
			name:        "other release generated dockercfg",
			secret:      corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test-other-consul-tls-init-dockercfg-abcde"}},
			wantDefault: true,
		},
		{
			name: "explicitly owned secret",
			secret: corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "custom-name", Labels: map[string]string{"release": "consul"},
			}},
			wantOCP: true,
		},
		{
			name: "other release with overlapping name",
			secret: corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "consul-other-ca-cert", Labels: map[string]string{"release": "consul-other"},
			}},
			wantDefault: true,
		},
		{
			name: "Helm ownership in another namespace",
			secret: corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "consul-ca-cert", Annotations: map[string]string{
					"meta.helm.sh/release-name": "consul", "meta.helm.sh/release-namespace": "other",
				},
			}},
			wantDefault: true,
		},
		{
			name:   "license",
			secret: corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: config.LicenseSecretName}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &HelmCluster{
				releaseName: "consul",
				helmOptions: &helm.Options{KubectlOptions: &terratestk8s.KubectlOptions{Namespace: "consul"}},
				cfg:         &config.TestConfig{},
			}
			require.Equal(t, tc.wantDefault, cluster.isReleaseSecret(tc.secret))
			cluster.cfg.EnableOpenshift = true
			require.Equal(t, tc.wantOCP, cluster.isReleaseSecret(tc.secret))
			cluster.cfg.EnableOpenshift = false
			cluster.cfg.UseOpenshift = true
			require.Equal(t, tc.wantOCP, cluster.isReleaseSecret(tc.secret))
		})
	}
}
