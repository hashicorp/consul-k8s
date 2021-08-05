package environment

import (
	"fmt"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/config"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	DefaultContextName   = "default"
	SecondaryContextName = "secondary"
)

// TestEnvironment represents the infrastructure environment of the test,
// such as the kubernetes cluster(s) the test is running against
type TestEnvironment interface {
	DefaultContext(t *testing.T) TestContext
	Context(t *testing.T, name string) TestContext
}

// TestContext represents a specific context a test needs,
// for example, information about a specific Kubernetes cluster.
type TestContext interface {
	KubectlOptions(t *testing.T) *k8s.KubectlOptions
	KubernetesClient(t *testing.T) kubernetes.Interface
}

type KubernetesEnvironment struct {
	contexts map[string]*kubernetesContext
}

func NewKubernetesEnvironmentFromConfig(config *config.TestConfig) *KubernetesEnvironment {
	defaultContext := NewContext(config.KubeNamespace, config.Kubeconfig, config.KubeContext)

	// Create a kubernetes environment with default context.
	kenv := &KubernetesEnvironment{
		contexts: map[string]*kubernetesContext{
			DefaultContextName: defaultContext,
		},
	}

	// Add secondary context if multi cluster tests are enabled.
	if config.EnableMultiCluster {
		kenv.contexts[SecondaryContextName] = NewContext(config.SecondaryKubeNamespace, config.SecondaryKubeconfig, config.SecondaryKubeContext)
	}

	return kenv
}

func NewKubernetesEnvironmentFromContext(context *kubernetesContext) *KubernetesEnvironment {
	// Create a kubernetes environment with default context.
	kenv := &KubernetesEnvironment{
		contexts: map[string]*kubernetesContext{
			DefaultContextName: context,
		},
	}

	return kenv
}

func (k *KubernetesEnvironment) Context(t *testing.T, name string) TestContext {
	ctx, ok := k.contexts[name]
	require.Truef(t, ok, fmt.Sprintf("requested context %s not found", name))

	return ctx
}

func (k *KubernetesEnvironment) DefaultContext(t *testing.T) TestContext {
	ctx, ok := k.contexts[DefaultContextName]
	require.Truef(t, ok, "default context not found")

	return ctx
}

type kubernetesContext struct {
	pathToKubeConfig string
	kubeContextName  string
	namespace        string

	client  kubernetes.Interface
	options *k8s.KubectlOptions
}

func (k kubernetesContext) KubectlOptions(t *testing.T) *k8s.KubectlOptions {
	if k.options != nil {
		return k.options
	}

	k.options = &k8s.KubectlOptions{
		ContextName: k.kubeContextName,
		ConfigPath:  k.pathToKubeConfig,
		Namespace:   k.namespace,
	}

	// If namespace is not explicitly set via flags,
	// set it either from context or to the "default" namespace.
	if k.namespace == "" {
		configPath, err := k.options.GetConfigPath(t)
		require.NoError(t, err)

		rawConfig, err := k8s.LoadConfigFromPath(configPath).RawConfig()
		require.NoError(t, err)

		contextName := helpers.KubernetesContextFromOptions(t, k.options)
		if rawConfig.Contexts[contextName].Namespace != "" {
			k.options.Namespace = rawConfig.Contexts[contextName].Namespace
		} else {
			k.options.Namespace = metav1.NamespaceDefault
		}
	}
	return k.options
}

func (k kubernetesContext) KubernetesClient(t *testing.T) kubernetes.Interface {
	if k.client != nil {
		return k.client
	}

	k.client = helpers.KubernetesClientFromOptions(t, k.KubectlOptions(t))

	return k.client
}

func NewContext(namespace, pathToKubeConfig, kubeContextName string) *kubernetesContext {
	return &kubernetesContext{
		namespace:        namespace,
		pathToKubeConfig: pathToKubeConfig,
		kubeContextName:  kubeContextName,
	}
}
