package framework

import (
	"flag"
	"sync"
)

type TestFlags struct {
	flagKubeconfig  string
	flagKubecontext string
	flagNamespace   string

	flagConsulImage    string
	flagConsulK8sImage string

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
}

func (t *TestFlags) testConfigFromFlags() *TestConfig {
	return &TestConfig{
		Kubeconfig:    t.flagKubeconfig,
		KubeContext:   t.flagKubecontext,
		KubeNamespace: t.flagNamespace,

		ConsulImage: t.flagConsulImage,
		ConsulK8SImage: t.flagConsulK8sImage,
	}
}
