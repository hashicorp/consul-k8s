// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package environment

import (
	"fmt"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	DefaultContextIndex = 0
)

// TestEnvironment represents the infrastructure environment of the test,
// such as the kubernetes cluster(s) the test is running against.
type TestEnvironment interface {
	DefaultContext(t *testing.T) TestContext
	Context(t *testing.T, index int) TestContext
}

// TestContext represents a specific context a test needs,
// for example, information about a specific Kubernetes cluster.
type TestContext interface {
	KubectlOptions(t *testing.T) *k8s.KubectlOptions
	// TODO: I don't love this.
	KubectlOptionsForNamespace(ns string) *k8s.KubectlOptions
	KubernetesClient(t *testing.T) kubernetes.Interface
	ControllerRuntimeClient(t *testing.T) client.Client
}

type KubernetesEnvironment struct {
	contexts []*kubernetesContext
}

func NewKubernetesEnvironmentFromConfig(config *config.TestConfig) *KubernetesEnvironment {
	// First kubeEnv is the default
	defaultContext := NewContext(config.GetPrimaryKubeEnv().KubeNamespace, config.GetPrimaryKubeEnv().KubeConfig, config.GetPrimaryKubeEnv().KubeContext)

	// Create a kubernetes environment with default context.
	kenv := &KubernetesEnvironment{
		contexts: []*kubernetesContext{
			defaultContext,
		},
	}

	// Add additional contexts if multi cluster tests are enabled.
	if config.EnableMultiCluster {
		for _, v := range config.KubeEnvs[1:] {
			kenv.contexts = append(kenv.contexts, NewContext(v.KubeNamespace, v.KubeConfig, v.KubeContext))
		}
	}

	return kenv
}

func (k *KubernetesEnvironment) Context(t *testing.T, index int) TestContext {
	lenContexts := len(k.contexts)
	require.Greater(t, lenContexts, index, fmt.Sprintf("context list does not contain an index %d, length is %d", index, lenContexts))
	return k.contexts[index]
}

func (k *KubernetesEnvironment) DefaultContext(t *testing.T) TestContext {
	lenContexts := len(k.contexts)
	require.Greater(t, lenContexts, DefaultContextIndex, fmt.Sprintf("context list does not contain an index %d, length is %d", DefaultContextIndex, lenContexts))
	return k.contexts[DefaultContextIndex]
}

type kubernetesContext struct {
	pathToKubeConfig string
	kubeContextName  string
	namespace        string

	client        kubernetes.Interface
	runtimeClient client.Client

	options *k8s.KubectlOptions
}

// KubernetesContextFromOptions returns the Kubernetes context from options.
// If context is explicitly set in options, it returns that context.
// Otherwise, it returns the current context.
func KubernetesContextFromOptions(t *testing.T, options *k8s.KubectlOptions) string {
	t.Helper()

	// First, check if context set in options and return that
	if options.ContextName != "" {
		return options.ContextName
	}

	// Otherwise, get current context from config
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)

	rawConfig, err := k8s.LoadConfigFromPath(configPath).RawConfig()
	require.NoError(t, err)

	return rawConfig.CurrentContext
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

		contextName := KubernetesContextFromOptions(t, k.options)
		if rawConfig.Contexts[contextName].Namespace != "" {
			k.options.Namespace = rawConfig.Contexts[contextName].Namespace
		} else {
			k.options.Namespace = metav1.NamespaceDefault
		}
	}
	return k.options
}

func (k kubernetesContext) KubectlOptionsForNamespace(ns string) *k8s.KubectlOptions {
	return &k8s.KubectlOptions{
		ContextName: k.kubeContextName,
		ConfigPath:  k.pathToKubeConfig,
		Namespace:   ns,
	}
}

// KubernetesClientFromOptions takes KubectlOptions and returns Kubernetes API client.
func KubernetesClientFromOptions(t *testing.T, options *k8s.KubectlOptions) kubernetes.Interface {
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)

	config, err := k8s.LoadApiClientConfigE(configPath, options.ContextName)
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	return client
}

func (k kubernetesContext) KubernetesClient(t *testing.T) kubernetes.Interface {
	if k.client != nil {
		return k.client
	}

	k.client = KubernetesClientFromOptions(t, k.KubectlOptions(t))

	return k.client
}

func (k kubernetesContext) ControllerRuntimeClient(t *testing.T) client.Client {
	if k.runtimeClient != nil {
		return k.runtimeClient
	}

	options := k.KubectlOptions(t)
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)
	config, err := k8s.LoadApiClientConfigE(configPath, options.ContextName)
	require.NoError(t, err)

	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, gwv1alpha2.Install(s))
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	client, err := client.New(config, client.Options{Scheme: s})
	require.NoError(t, err)

	k.runtimeClient = client

	return k.runtimeClient
}

func NewContext(namespace, pathToKubeConfig, kubeContextName string) *kubernetesContext {
	return &kubernetesContext{
		namespace:        namespace,
		pathToKubeConfig: pathToKubeConfig,
		kubeContextName:  kubeContextName,
	}
}
