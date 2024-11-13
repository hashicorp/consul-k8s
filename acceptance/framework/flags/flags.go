// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package flags

import (
	"errors"
	"flag"
	"os"
	"strings"
	"sync"

	"github.com/hashicorp/go-version"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
)

type TestFlags struct {
	flagKubeconfigs        listFlag
	flagKubecontexts       listFlag
	flagKubeNamespaces     listFlag
	flagEnableMultiCluster bool

	flagEnableEnterprise  bool
	flagEnterpriseLicense string

	flagEnableOpenshift bool

	flagSkipDatadogTests bool

	flagEnablePodSecurityPolicies bool

	flagEnableCNI                      bool
	flagEnableRestrictedPSAEnforcement bool

	flagEnableTransparentProxy bool

	flagHelmChartVersion       string
	flagConsulImage            string
	flagConsulK8sImage         string
	flagConsulDataplaneImage   string
	flagConsulVersion          string
	flagConsulDataplaneVersion string
	flagEnvoyImage             string
	flagConsulCollectorImage   string
	flagVaultHelmChartVersion  string
	flagVaultServerVersion     string

	flagHCPResourceID string

	flagNoCleanupOnFailure bool
	flagNoCleanup          bool

	flagDebugDirectory string

	flagUseAKS          bool
	flagUseEKS          bool
	flagUseGKE          bool
	flagUseGKEAutopilot bool
	flagUseKind         bool
	flagUseOpenshift    bool

	flagDisablePeering bool

	once sync.Once
}

func NewTestFlags() *TestFlags {
	t := &TestFlags{}
	t.once.Do(t.init)

	return t
}

type listFlag []string

// String() returns a comma separated list in the form "var1,var2,var3".
func (f *listFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *listFlag) Set(value string) error {
	*f = strings.Split(value, ",")
	return nil
}

func (t *TestFlags) init() {
	flag.StringVar(&t.flagConsulImage, "consul-image", "", "The Consul image to use for all tests.")
	flag.StringVar(&t.flagConsulK8sImage, "consul-k8s-image", "", "The consul-k8s image to use for all tests.")
	flag.StringVar(&t.flagConsulDataplaneImage, "consul-dataplane-image", "", "The consul-dataplane image to use for all tests.")
	flag.StringVar(&t.flagConsulVersion, "consul-version", "", "The consul version used for all tests.")
	flag.StringVar(&t.flagConsulDataplaneVersion, "consul-dataplane-version", "", "The consul-dataplane version used for all tests.")
	flag.StringVar(&t.flagHelmChartVersion, "helm-chart-version", config.HelmChartPath, "The helm chart used for all tests.")
	flag.StringVar(&t.flagEnvoyImage, "envoy-image", "", "The Envoy image to use for all tests.")
	flag.StringVar(&t.flagConsulCollectorImage, "consul-collector-image", "", "The consul collector image to use for all tests.")
	flag.StringVar(&t.flagVaultServerVersion, "vault-server-version", "", "The vault serverversion used for all tests.")
	flag.StringVar(&t.flagVaultHelmChartVersion, "vault-helm-chart-version", "", "The Vault helm chart used for all tests.")

	flag.Var(&t.flagKubeconfigs, "kubeconfigs", "The list of paths to a kubeconfig files. If this is blank, "+
		"the default kubeconfig path (~/.kube/config) will be used.")
	flag.Var(&t.flagKubecontexts, "kube-contexts", "The list of names of the Kubernetes contexts to use. If this is blank, "+
		"the context set as the current context will be used by default.")
	flag.Var(&t.flagKubeNamespaces, "kube-namespaces", "The list of Kubernetes namespaces to use for tests.")
	flag.StringVar(&t.flagHCPResourceID, "hcp-resource-id", "", "The hcp resource id to use for all tests.")

	flag.BoolVar(&t.flagEnableMultiCluster, "enable-multi-cluster", false,
		"If true, the tests that require multiple Kubernetes clusters will be run. "+
			"The lists -kubeconfig or -kube-context must contain more than one entry when this flag is used.")

	flag.BoolVar(&t.flagEnableEnterprise, "enable-enterprise", false,
		"If true, the test suite will run tests for enterprise features. "+
			"Note that some features may require setting the enterprise license flag below or the env var CONSUL_ENT_LICENSE")
	flag.StringVar(&t.flagEnterpriseLicense, "enterprise-license", "",
		"The enterprise license for Consul.")

	flag.BoolVar(&t.flagEnableOpenshift, "enable-openshift", false,
		"If true, the tests will automatically add Openshift Helm value for each Helm install.")

	flag.BoolVar(&t.flagEnablePodSecurityPolicies, "enable-pod-security-policies", false,
		"If true, the test suite will run tests with pod security policies enabled.")

	flag.BoolVar(&t.flagEnableCNI, "enable-cni", false,
		"If true, the test suite will run tests with consul-cni plugin enabled. "+
			"In general, this will only run against tests that are mesh related (connect, mesh-gateway, peering, etc")

	flag.BoolVar(&t.flagEnableRestrictedPSAEnforcement, "enable-restricted-psa-enforcement", false,
		"If true, deploy Consul into a namespace with restricted PSA enforcement enabled. "+
			"The Consul namespaces (-kube-namespaces) will be configured with restricted PSA enforcement. "+
			"The CNI and test applications are deployed in different namespaces because they need more privilege than is allowed in a restricted namespace. "+
			"The CNI will be deployed into the kube-system namespace, which is a privileged namespace that should always exist. "+
			"Test applications are deployed, by default, into a namespace named '<consul-namespace>-apps' instead of the Consul namespace.")

	flag.BoolVar(&t.flagEnableTransparentProxy, "enable-transparent-proxy", false,
		"If true, the test suite will run tests with transparent proxy enabled. "+
			"This applies only to tests that enable connectInject.")

	flag.BoolVar(&t.flagNoCleanupOnFailure, "no-cleanup-on-failure", false,
		"If true, the tests will not cleanup Kubernetes resources they create when they finish running."+
			"Note this flag must be run with -failfast flag, otherwise subsequent tests will fail.")

	flag.BoolVar(&t.flagNoCleanup, "no-cleanup", false,
		"If true, the tests will not cleanup Kubernetes resources for Vault test")

	flag.StringVar(&t.flagDebugDirectory, "debug-directory", "", "The directory where to write debug information about failed test runs, "+
		"such as logs and pod definitions. If not provided, a temporary directory will be created by the tests.")

	flag.BoolVar(&t.flagUseAKS, "use-aks", false,
		"If true, the tests will assume they are running against an AKS cluster(s).")
	flag.BoolVar(&t.flagUseEKS, "use-eks", false,
		"If true, the tests will assume they are running against an EKS cluster(s).")
	flag.BoolVar(&t.flagUseGKE, "use-gke", false,
		"If true, the tests will assume they are running against a GKE cluster(s).")
	flag.BoolVar(&t.flagUseGKEAutopilot, "use-gke-autopilot", false,
		"If true, the tests will assume they are running against a GKE Autopilot cluster(s).")

	flag.BoolVar(&t.flagUseKind, "use-kind", false,
		"If true, the tests will assume they are running against a local kind cluster(s).")

	flag.BoolVar(&t.flagUseOpenshift, "use-openshift", false,
		"If true, the tests will assume they are running against an openshift cluster(s).")

	flag.BoolVar(&t.flagDisablePeering, "disable-peering", false,
		"If true, the peering tests will not run.")

	flag.BoolVar(&t.flagSkipDatadogTests, "skip-datadog", false,
		"If true, datadog acceptance tests will not run.")

	if t.flagEnterpriseLicense == "" {
		t.flagEnterpriseLicense = os.Getenv("CONSUL_ENT_LICENSE")
	}
}

func (t *TestFlags) Validate() error {
	if t.flagEnableMultiCluster {
		if len(t.flagKubecontexts) <= 1 && len(t.flagKubeconfigs) <= 1 {
			return errors.New("at least two contexts must be included in -kube-contexts or -kubeconfigs if -enable-multi-cluster is set")
		}
	}

	if len(t.flagKubecontexts) != 0 && len(t.flagKubeconfigs) != 0 {
		if len(t.flagKubecontexts) != len(t.flagKubeconfigs) {
			return errors.New("-kube-contexts and -kubeconfigs are both set but are not of equal length")
		}
	}

	if len(t.flagKubecontexts) != 0 && len(t.flagKubeNamespaces) != 0 {
		if len(t.flagKubecontexts) != len(t.flagKubeNamespaces) {
			return errors.New("-kube-contexts and -kube-namespaces are both set but are not of equal length")
		}
	}

	if len(t.flagKubeNamespaces) != 0 && len(t.flagKubeconfigs) != 0 {
		if len(t.flagKubeNamespaces) != len(t.flagKubeconfigs) {
			return errors.New("-kube-namespaces and -kubeconfigs are both set but are not of equal length")
		}
	}

	if t.flagEnableEnterprise && t.flagEnterpriseLicense == "" {
		return errors.New("-enable-enterprise provided without setting env var CONSUL_ENT_LICENSE with consul license")
	}

	return nil
}

func (t *TestFlags) TestConfigFromFlags() *config.TestConfig {
	tempDir := t.flagDebugDirectory

	// if the Version is empty consulVersion will be nil
	consulVersion, _ := version.NewVersion(t.flagConsulVersion)
	consulDataplaneVersion, _ := version.NewVersion(t.flagConsulDataplaneVersion)
	kubeEnvs := config.NewKubeTestConfigList(t.flagKubeconfigs, t.flagKubecontexts, t.flagKubeNamespaces)

	c := &config.TestConfig{
		EnableEnterprise:  t.flagEnableEnterprise,
		EnterpriseLicense: t.flagEnterpriseLicense,

		KubeEnvs:           kubeEnvs,
		EnableMultiCluster: t.flagEnableMultiCluster,

		EnableOpenshift: t.flagEnableOpenshift,

		SkipDataDogTests: t.flagSkipDatadogTests,

		EnablePodSecurityPolicies: t.flagEnablePodSecurityPolicies,

		EnableCNI:                      t.flagEnableCNI,
		EnableRestrictedPSAEnforcement: t.flagEnableRestrictedPSAEnforcement,

		EnableTransparentProxy: t.flagEnableTransparentProxy,

		DisablePeering: t.flagDisablePeering,

		HelmChartVersion:       t.flagHelmChartVersion,
		ConsulImage:            t.flagConsulImage,
		ConsulK8SImage:         t.flagConsulK8sImage,
		ConsulDataplaneImage:   t.flagConsulDataplaneImage,
		ConsulVersion:          consulVersion,
		ConsulDataplaneVersion: consulDataplaneVersion,
		EnvoyImage:             t.flagEnvoyImage,
		ConsulCollectorImage:   t.flagConsulCollectorImage,
		VaultHelmChartVersion:  t.flagVaultHelmChartVersion,
		VaultServerVersion:     t.flagVaultServerVersion,

		HCPResourceID: t.flagHCPResourceID,

		NoCleanupOnFailure: t.flagNoCleanupOnFailure,
		NoCleanup:          t.flagNoCleanup,
		DebugDirectory:     tempDir,
		UseAKS:             t.flagUseAKS,
		UseEKS:             t.flagUseEKS,
		UseGKE:             t.flagUseGKE,
		UseGKEAutopilot:    t.flagUseGKEAutopilot,
		UseKind:            t.flagUseKind,
		UseOpenshift:       t.flagUseOpenshift,
	}

	return c
}
