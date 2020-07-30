package framework

import (
	"errors"
	"flag"
	"sync"
)

type TestFlags struct {
	flagKubeconfig  string
	flagKubecontext string
	flagNamespace   string

	flagEnableMultiCluster   bool
	flagSecondaryKubeconfig  string
	flagSecondaryKubecontext string
	flagSecondaryNamespace   string

	flagConsulImage    string
	flagConsulK8sImage string

	flagNoCleanupOnFailure bool

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
	flag.StringVar(&t.flagNamespace, "namespace", "default", "The Kubernetes namespace to use for tests.")

	flag.StringVar(&t.flagConsulImage, "consul-image", "", "The Consul image to use for all tests.")
	flag.StringVar(&t.flagConsulK8sImage, "consul-k8s-image", "", "The consul-k8s image to use for all tests.")

	flag.BoolVar(&t.flagEnableMultiCluster, "enable-multi-cluster", false,
		"If true, the tests that require multiple Kubernetes clusters will be run. "+
			"At least one of -secondary-kubeconfig or -secondary-kubecontext is required when this flag is used.")
	flag.StringVar(&t.flagSecondaryKubeconfig, "secondary-kubeconfig", "", "The path to a kubeconfig file of the secondary k8s cluster. "+
		"If this is blank, the default kubeconfig path (~/.kube/config) will be used.")
	flag.StringVar(&t.flagSecondaryKubecontext, "secondary-kubecontext", "", "The name of the Kubernetes context for the secondary cluster to use. "+
		"If this is blank, the context set as the current context will be used by default.")
	flag.StringVar(&t.flagSecondaryNamespace, "secondary-namespace", "default", "The Kubernetes namespace to use in the secondary k8s cluster.")

	flag.BoolVar(&t.flagNoCleanupOnFailure, "no-cleanup-on-failure", false,
		"If true, the tests will not cleanup resources they create when they finish running."+
			"Note this flag must be run with -failfast flag, otherwise subsequent tests will fail.")
}

func (t *TestFlags) validate() error {
	if t.flagEnableMultiCluster {
		if t.flagSecondaryKubecontext == "" && t.flagSecondaryKubeconfig == "" {
			return errors.New("at least one of -secondary-kubecontext or -secondary-kubeconfig flags must be provided if -enable-multi-cluster is set")
		}
	}
	return nil
}

func (t *TestFlags) testConfigFromFlags() *TestConfig {
	return &TestConfig{
		Kubeconfig:    t.flagKubeconfig,
		KubeContext:   t.flagKubecontext,
		KubeNamespace: t.flagNamespace,

		EnableMultiCluster:     t.flagEnableMultiCluster,
		SecondaryKubeconfig:    t.flagSecondaryKubeconfig,
		SecondaryKubeContext:   t.flagSecondaryKubecontext,
		SecondaryKubeNamespace: t.flagSecondaryNamespace,

		ConsulImage:    t.flagConsulImage,
		ConsulK8SImage: t.flagConsulK8sImage,

		NoCleanupOnFailure: t.flagNoCleanupOnFailure,
	}
}
