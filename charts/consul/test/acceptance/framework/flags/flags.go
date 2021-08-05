package flags

import (
	"errors"
	"flag"
	"os"
	"sync"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/config"
)

type TestFlags struct {
	flagKubeconfig  string
	flagKubecontext string
	flagNamespace   string

	flagEnableMultiCluster   bool
	flagSecondaryKubeconfig  string
	flagSecondaryKubecontext string
	flagSecondaryNamespace   string

	flagEnableEnterprise  bool
	flagEnterpriseLicense string

	flagEnableOpenshift bool

	flagEnablePodSecurityPolicies bool

	flagEnableTransparentProxy bool

	flagConsulImage    string
	flagConsulK8sImage string

	flagNoCleanupOnFailure bool

	flagDebugDirectory string

	flagUseKind bool

	once sync.Once
}

func NewTestFlags() *TestFlags {
	t := &TestFlags{}
	t.once.Do(t.init)

	return t
}

func (t *TestFlags) init() {
	flag.StringVar(&t.flagKubeconfig, "kubeconfig", "", "The path to a kubeconfig file. If this is blank, "+
		"the default kubeconfig path (~/.kube/config) will be used.")
	flag.StringVar(&t.flagKubecontext, "kubecontext", "", "The name of the Kubernetes context to use. If this is blank, "+
		"the context set as the current context will be used by default.")
	flag.StringVar(&t.flagNamespace, "namespace", "", "The Kubernetes namespace to use for tests.")

	flag.StringVar(&t.flagConsulImage, "consul-image", "", "The Consul image to use for all tests.")
	flag.StringVar(&t.flagConsulK8sImage, "consul-k8s-image", "", "The consul-k8s image to use for all tests.")

	flag.BoolVar(&t.flagEnableMultiCluster, "enable-multi-cluster", false,
		"If true, the tests that require multiple Kubernetes clusters will be run. "+
			"At least one of -secondary-kubeconfig or -secondary-kubecontext is required when this flag is used.")
	flag.StringVar(&t.flagSecondaryKubeconfig, "secondary-kubeconfig", "", "The path to a kubeconfig file of the secondary k8s cluster. "+
		"If this is blank, the default kubeconfig path (~/.kube/config) will be used.")
	flag.StringVar(&t.flagSecondaryKubecontext, "secondary-kubecontext", "", "The name of the Kubernetes context for the secondary cluster to use. "+
		"If this is blank, the context set as the current context will be used by default.")
	flag.StringVar(&t.flagSecondaryNamespace, "secondary-namespace", "", "The Kubernetes namespace to use in the secondary k8s cluster.")

	flag.BoolVar(&t.flagEnableEnterprise, "enable-enterprise", false,
		"If true, the test suite will run tests for enterprise features. "+
			"Note that some features may require setting the enterprise license flag below or the env var CONSUL_ENT_LICENSE")
	flag.StringVar(&t.flagEnterpriseLicense, "enterprise-license", "",
		"The enterprise license for Consul.")

	flag.BoolVar(&t.flagEnableOpenshift, "enable-openshift", false,
		"If true, the tests will automatically add Openshift Helm value for each Helm install.")

	flag.BoolVar(&t.flagEnablePodSecurityPolicies, "enable-pod-security-policies", false,
		"If true, the test suite will run tests with pod security policies enabled.")

	flag.BoolVar(&t.flagEnableTransparentProxy, "enable-transparent-proxy", false,
		"If true, the test suite will run tests with transparent proxy enabled. "+
			"This applies only to tests that enable connectInject.")

	flag.BoolVar(&t.flagNoCleanupOnFailure, "no-cleanup-on-failure", false,
		"If true, the tests will not cleanup Kubernetes resources they create when they finish running."+
			"Note this flag must be run with -failfast flag, otherwise subsequent tests will fail.")

	flag.StringVar(&t.flagDebugDirectory, "debug-directory", "", "The directory where to write debug information about failed test runs, "+
		"such as logs and pod definitions. If not provided, a temporary directory will be created by the tests.")

	flag.BoolVar(&t.flagUseKind, "use-kind", false,
		"If true, the tests will assume they are running against a local kind cluster(s).")

	if t.flagEnterpriseLicense == "" {
		t.flagEnterpriseLicense = os.Getenv("CONSUL_ENT_LICENSE")
	}
}

func (t *TestFlags) Validate() error {
	if t.flagEnableMultiCluster {
		if t.flagSecondaryKubecontext == "" && t.flagSecondaryKubeconfig == "" {
			return errors.New("at least one of -secondary-kubecontext or -secondary-kubeconfig flags must be provided if -enable-multi-cluster is set")
		}
	}

	if t.flagEnableEnterprise && t.flagEnterpriseLicense == "" {
		return errors.New("-enable-enterprise provided without setting env var CONSUL_ENT_LICENSE with consul license")
	}
	return nil
}

func (t *TestFlags) TestConfigFromFlags() *config.TestConfig {
	tempDir := t.flagDebugDirectory

	return &config.TestConfig{
		Kubeconfig:    t.flagKubeconfig,
		KubeContext:   t.flagKubecontext,
		KubeNamespace: t.flagNamespace,

		EnableMultiCluster:     t.flagEnableMultiCluster,
		SecondaryKubeconfig:    t.flagSecondaryKubeconfig,
		SecondaryKubeContext:   t.flagSecondaryKubecontext,
		SecondaryKubeNamespace: t.flagSecondaryNamespace,

		EnableEnterprise:  t.flagEnableEnterprise,
		EnterpriseLicense: t.flagEnterpriseLicense,

		EnableOpenshift: t.flagEnableOpenshift,

		EnablePodSecurityPolicies: t.flagEnablePodSecurityPolicies,

		EnableTransparentProxy: t.flagEnableTransparentProxy,

		ConsulImage:    t.flagConsulImage,
		ConsulK8SImage: t.flagConsulK8sImage,

		NoCleanupOnFailure: t.flagNoCleanupOnFailure,
		DebugDirectory:     tempDir,
		UseKind:            t.flagUseKind,
	}
}
