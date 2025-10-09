// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/go-version"
	"gopkg.in/yaml.v2"
)

// HelmChartPath is the path to the helm chart.
// Note: this will need to be changed if this file is moved.
const (
	HelmChartPath     = "../../../charts/consul"
	LicenseSecretName = "license"
	LicenseSecretKey  = "key"
)

type KubeTestConfig struct {
	KubeConfig    string
	KubeContext   string
	KubeNamespace string
}

// NewKubeTestConfigList takes lists of kubernetes configs, contexts and namespaces and constructs KubeTestConfig
// We validate ahead of time that the lists are either 0 or the same length as we expect that if the length of a list
// is greater than 0, then the indexes should match. For example: []kubeContexts{"ctx1", "ctx2"} indexes 0, 1 match with []kubeNamespaces{"ns1", "ns2"}.
func NewKubeTestConfigList(kubeConfigs, kubeContexts, kubeNamespaces []string) []KubeTestConfig {
	// Grab the longest length.
	l := math.Max(float64(len(kubeConfigs)),
		math.Max(float64(len(kubeContexts)), float64(len(kubeNamespaces))))

	// If all are empty, then return a single empty entry
	if l == 0 {
		return []KubeTestConfig{{}}
	}

	// Add each non-zero length list to the new structs, we should have
	// n structs where n == l.
	out := make([]KubeTestConfig, int(l))
	for i := range out {
		kenv := KubeTestConfig{}
		if len(kubeConfigs) != 0 {
			kenv.KubeConfig = kubeConfigs[i]
		}
		if len(kubeContexts) != 0 {
			kenv.KubeContext = kubeContexts[i]
		}
		if len(kubeNamespaces) != 0 {
			kenv.KubeNamespace = kubeNamespaces[i]
		}
		out[i] = kenv
	}
	return out
}

// TestConfig holds configuration for the test suite.
type TestConfig struct {
	KubeEnvs           []KubeTestConfig
	EnableMultiCluster bool

	EnableEnterprise  bool
	EnterpriseLicense string

	SkipDataDogTests        bool
	DatadogHelmChartVersion string

	EnableOpenshift bool

	EnablePodSecurityPolicies bool

	EnableCNI                      bool
	EnableRestrictedPSAEnforcement bool

	EnableTransparentProxy bool

	DisablePeering bool

	HelmChartVersion       string
	ConsulImage            string
	ConsulK8SImage         string
	ConsulDataplaneImage   string
	ConsulVersion          *version.Version
	ConsulDataplaneVersion *version.Version
	EnvoyImage             string
	ConsulCollectorImage   string

	HCPResourceID string

	VaultHelmChartVersion string
	VaultServerVersion    string

	NoCleanupOnFailure bool
	NoCleanup          bool
	DebugDirectory     string

	UseAKS          bool
	UseEKS          bool
	UseGKE          bool
	UseGKEAutopilot bool
	UseKind         bool
	UseOpenshift    bool

	helmChartPath string
	DualStack     bool
}

func (t *TestConfig) GetDualStack() string {
	if t.DualStack {
		return "true"
	}
	return "false"
}

// HelmValuesFromConfig returns a map of Helm values
// that includes any non-empty values from the TestConfig.
func (t *TestConfig) HelmValuesFromConfig() (map[string]string, error) {
	helmValues := map[string]string{}

	// Set the enterprise image first if enterprise tests are enabled.
	// It can be overwritten by the -consul-image flag later.
	if t.EnableEnterprise {
		entImage, err := t.entImage()
		if err != nil {
			return nil, err
		}
		setIfNotEmpty(helmValues, "global.image", entImage)

		if t.EnterpriseLicense != "" {
			setIfNotEmpty(helmValues, "global.enterpriseLicense.secretName", LicenseSecretName)
			setIfNotEmpty(helmValues, "global.enterpriseLicense.secretKey", LicenseSecretKey)
		}
	}

	if t.EnableOpenshift {
		setIfNotEmpty(helmValues, "global.openshift.enabled", "true")
	}

	if t.EnablePodSecurityPolicies {
		setIfNotEmpty(helmValues, "global.enablePodSecurityPolicies", "true")
	}

	if t.EnableCNI {
		setIfNotEmpty(helmValues, "connectInject.cni.enabled", "true")
		setIfNotEmpty(helmValues, "connectInject.cni.logLevel", "debug")
		// GKE is currently the only cloud provider that uses a different CNI bin dir.
		if t.UseGKE {
			setIfNotEmpty(helmValues, "connectInject.cni.cniBinDir", "/home/kubernetes/bin")
		}
		if t.EnableOpenshift {
			setIfNotEmpty(helmValues, "connectInject.cni.multus", "true")
			setIfNotEmpty(helmValues, "connectInject.cni.cniBinDir", "/var/lib/cni/bin")
			setIfNotEmpty(helmValues, "connectInject.cni.cniNetDir", "/etc/kubernetes/cni/net.d")
		}

		if t.EnableRestrictedPSAEnforcement {
			// The CNI requires privilege, so when restricted PSA enforcement is enabled on the Consul
			// namespace it must be run in a different privileged namespace.
			setIfNotEmpty(helmValues, "connectInject.cni.namespace", "kube-system")
		}
	}

	fmt.Println("===========================> \n", string(debug.Stack()))

	if t.DualStack {
		fmt.Println("===========================> Dual stack mode set to true")
		setIfNotEmpty(helmValues, "global.dualStack.defaultEnabled", "true")
	} else {
		fmt.Println("===========================> Dual stack mode set to false false false")
	}

	// UseGKEAutopilot is a temporary hack that we need in place as GKE Autopilot is already installing
	// Gateway CRDs in the clusters. There are still other CRDs we need to install though (see helm cluster install)
	if t.UseGKEAutopilot {
		setIfNotEmpty(helmValues, "global.server.resources.requests.cpu", "500m")
		setIfNotEmpty(helmValues, "global.server.resources.limits.cpu", "500m")
		setIfNotEmpty(helmValues, "connectInject.apiGateway.manageExternalCRDs", "false")
		setIfNotEmpty(helmValues, "connectInject.apiGateway.manageNonStandardCRDs", "true")
	}

	setIfNotEmpty(helmValues, "connectInject.transparentProxy.defaultEnabled", strconv.FormatBool(t.EnableTransparentProxy))

	setIfNotEmpty(helmValues, "global.image", t.ConsulImage)
	setIfNotEmpty(helmValues, "global.imageK8S", t.ConsulK8SImage)
	setIfNotEmpty(helmValues, "global.imageEnvoy", t.EnvoyImage)
	setIfNotEmpty(helmValues, "global.imageConsulDataplane", t.ConsulDataplaneImage)

	return helmValues, nil
}

// IsExpectedClusterCount check that we have at least the required number of clusters to
// run a test.
func (t *TestConfig) IsExpectedClusterCount(count int) bool {
	return len(t.KubeEnvs) >= count
}

// GetPrimaryKubeEnv returns the primary Kubernetes environment.
func (t *TestConfig) GetPrimaryKubeEnv() KubeTestConfig {
	// Return the first in the list as this is always the primary
	// kube environment. If empty return an empty kubeEnv
	if len(t.KubeEnvs) < 1 {
		return KubeTestConfig{}
	} else {
		return t.KubeEnvs[0]
	}
}

type values struct {
	Global globalValues `yaml:"global"`
}

type globalValues struct {
	Image string `yaml:"image"`
}

// entImage parses out consul version from values.yaml
// and sets global.image to the consul enterprise image with that version.
func (t *TestConfig) entImage() (string, error) {
	if t.helmChartPath == "" {
		t.helmChartPath = HelmChartPath
	}

	// Unmarshal values.yaml to current global.image value.
	valuesContents, err := os.ReadFile(filepath.Join(t.helmChartPath, "values.yaml"))
	if err != nil {
		return "", err
	}

	var v values
	err = yaml.Unmarshal(valuesContents, &v)
	if err != nil {
		return "", err
	}

	// Check if the image contains digest instead of a tag.
	// If it does, we want to use that image instead rather than
	// trying to change the tag to an enterprise tag.
	if strings.Contains(v.Global.Image, "@sha256") {
		return v.Global.Image, nil
	}

	// Otherwise, assume that we have an image tag with a version in it.
	// Use the same Docker repository and tagging scheme, but replace 'consul' with 'consul-enterprise'.
	imageTag := strings.Replace(v.Global.Image, "/consul:", "/consul-enterprise:", 1)

	// We currently add an '-ent' suffix to release versions of enterprise images (nightly previews
	// do not include this suffix).
	if strings.HasPrefix(imageTag, "hashicorp/consul-enterprise:") {
		imageTag = fmt.Sprintf("%s-ent", imageTag)
	}

	return imageTag, nil
}

func (c *TestConfig) SkipWhenOpenshiftAndCNI(t *testing.T) {
	if c.EnableOpenshift && c.EnableCNI {
		t.Skip("skipping because -enable-cni and -enable-openshift are set and this test doesn't deploy apps correctly")
	}
}

func (c *TestConfig) SkipWhenCNI(t *testing.T) {
	if c.EnableCNI {
		t.Skip("skipping because -enable-cni is set and doesn't apply to this accepatance test")
	}
}

// setIfNotEmpty sets key to val in map m if value is not empty.
func setIfNotEmpty(m map[string]string, key, val string) {
	if val != "" {
		m[key] = val
	}
}
