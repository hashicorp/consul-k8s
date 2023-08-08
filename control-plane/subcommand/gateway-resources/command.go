// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatewayresources

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/mitchellh/cli"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// this dupes the Kubernetes tolerations
// struct with yaml tags for validation.
type toleration struct {
	Key               string `yaml:"key"`
	Operator          string `yaml:"operator"`
	Value             string `yaml:"value"`
	Effect            string `yaml:"effect"`
	TolerationSeconds *int64 `yaml:"tolerationSeconds"`
}

func tolerationToKubernetes(t toleration) corev1.Toleration {
	return corev1.Toleration{
		Key:               t.Key,
		Operator:          corev1.TolerationOperator(t.Operator),
		Value:             t.Value,
		Effect:            corev1.TaintEffect(t.Effect),
		TolerationSeconds: t.TolerationSeconds,
	}
}

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	flagHeritage               string
	flagChart                  string
	flagApp                    string
	flagRelease                string
	flagComponent              string
	flagControllerName         string
	flagGatewayClassName       string
	flagGatewayClassConfigName string

	flagServiceType                string
	flagDeploymentDefaultInstances int
	flagDeploymentMaxInstances     int
	flagDeploymentMinInstances     int

	flagNodeSelector       string // this is a yaml multiline string map
	flagTolerations        string // this is a multiline yaml string matching the tolerations array
	flagServiceAnnotations string // this is a multiline yaml string array of annotations to allow

	flagOpenshiftSCCName string

	flagMapPrivilegedContainerPorts int

	k8sClient client.Client

	once sync.Once
	help string

	nodeSelector       map[string]string
	tolerations        []corev1.Toleration
	serviceAnnotations []string

	ctx context.Context
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagGatewayClassName, "gateway-class-name", "",
		"Name of Kubernetes GatewayClass to ensure is created.")
	c.flags.StringVar(&c.flagGatewayClassConfigName, "gateway-class-config-name", "",
		"Name of Kubernetes GatewayClassConfig to ensure is created.")
	c.flags.StringVar(&c.flagHeritage, "heritage", "",
		"Helm chart heritage for created objects.")
	c.flags.StringVar(&c.flagChart, "chart", "",
		"Helm chart name for created objects.")
	c.flags.StringVar(&c.flagApp, "app", "",
		"Helm chart app for created objects.")
	c.flags.StringVar(&c.flagRelease, "release-name", "",
		"Helm chart release for created objects.")
	c.flags.StringVar(&c.flagComponent, "component", "",
		"Helm chart component for created objects.")
	c.flags.StringVar(&c.flagControllerName, "controller-name", "",
		"The controller name value to use in the GatewayClass.")
	c.flags.StringVar(&c.flagServiceType, "service-type", "",
		"The service type to use for a gateway deployment.",
	)
	c.flags.IntVar(&c.flagDeploymentDefaultInstances, "deployment-default-instances", 0,
		"The number of instances to deploy for each gateway by default.",
	)
	c.flags.IntVar(&c.flagDeploymentMaxInstances, "deployment-max-instances", 0,
		"The maximum number of instances to deploy for each gateway.",
	)
	c.flags.IntVar(&c.flagDeploymentMinInstances, "deployment-min-instances", 0,
		"The minimum number of instances to deploy for each gateway.",
	)
	c.flags.StringVar(&c.flagNodeSelector, "node-selector", "",
		"The node selector to use in scheduling a gateway.",
	)
	c.flags.StringVar(&c.flagTolerations, "tolerations", "",
		"The tolerations to use in a deployed gateway.",
	)
	c.flags.StringVar(&c.flagServiceAnnotations, "service-annotations", "",
		"The annotations to copy over from a gateway to its service.",
	)
	c.flags.StringVar(&c.flagOpenshiftSCCName, "openshift-scc-name", "",
		"Name of security context constraint to use for gateways on Openshift.",
	)
	c.flags.IntVar(&c.flagMapPrivilegedContainerPorts, "map-privileged-container-ports", 0,
		"The value to add to privileged container ports (< 1024) to avoid requiring addition privileges for the "+
			"gateway container.",
	)

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)
	if err = c.flags.Parse(args); err != nil {
		return 1
	}
	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	// Create the Kubernetes client
	if c.k8sClient == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}

		s := runtime.NewScheme()
		if err := clientgoscheme.AddToScheme(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add client-go schema: %s", err))
			return 1
		}
		if err := gwv1beta1.Install(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add api-gateway schema: %s", err))
			return 1
		}
		if err := v1alpha1.AddToScheme(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add consul-k8s schema: %s", err))
			return 1
		}

		c.k8sClient, err = client.New(config, client.Options{Scheme: s})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	// do the creation

	labels := map[string]string{
		"app":       c.flagApp,
		"chart":     c.flagChart,
		"heritage":  c.flagHeritage,
		"release":   c.flagRelease,
		"component": c.flagComponent,
	}
	classConfig := &v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{Name: c.flagGatewayClassConfigName, Labels: labels},
		Spec: v1alpha1.GatewayClassConfigSpec{
			ServiceType:  serviceTypeIfSet(c.flagServiceType),
			NodeSelector: c.nodeSelector,
			CopyAnnotations: v1alpha1.CopyAnnotationsSpec{
				Service: c.serviceAnnotations,
			},
			Tolerations: c.tolerations,
			DeploymentSpec: v1alpha1.DeploymentSpec{
				DefaultInstances: nonZeroOrNil(c.flagDeploymentDefaultInstances),
				MaxInstances:     nonZeroOrNil(c.flagDeploymentMaxInstances),
				MinInstances:     nonZeroOrNil(c.flagDeploymentMinInstances),
			},
			OpenshiftSCCName:            c.flagOpenshiftSCCName,
			MapPrivilegedContainerPorts: int32(c.flagMapPrivilegedContainerPorts),
		},
	}

	class := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: c.flagGatewayClassName, Labels: labels},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: gwv1beta1.GatewayController(c.flagControllerName),
			ParametersRef: &gwv1beta1.ParametersReference{
				Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
				Kind:  gwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
				Name:  c.flagGatewayClassConfigName,
			},
		},
	}

	if err := forceClassConfig(context.Background(), c.k8sClient, classConfig); err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	if err := forceClass(context.Background(), c.k8sClient, class); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	return 0
}

func (c *Command) validateFlags() error {
	if c.flagGatewayClassConfigName == "" {
		return errors.New("-gateway-class-config-name must be set")
	}
	if c.flagGatewayClassName == "" {
		return errors.New("-gateway-class-name must be set")
	}
	if c.flagHeritage == "" {
		return errors.New("-heritage must be set")
	}
	if c.flagChart == "" {
		return errors.New("-chart must be set")
	}
	if c.flagApp == "" {
		return errors.New("-app must be set")
	}
	if c.flagRelease == "" {
		return errors.New("-release-name must be set")
	}
	if c.flagComponent == "" {
		return errors.New("-component must be set")
	}
	if c.flagControllerName == "" {
		return errors.New("-controller-name must be set")
	}
	if c.flagTolerations != "" {
		var tolerations []toleration
		if err := yaml.Unmarshal([]byte(c.flagTolerations), &tolerations); err != nil {
			return fmt.Errorf("error decoding tolerations: %w", err)
		}
		c.tolerations = common.ConvertSliceFunc(tolerations, tolerationToKubernetes)
	}
	if c.flagNodeSelector != "" {
		if err := yaml.Unmarshal([]byte(c.flagNodeSelector), &c.nodeSelector); err != nil {
			return fmt.Errorf("error decoding node selector: %w", err)
		}
	}
	if c.flagNodeSelector != "" {
		if err := yaml.Unmarshal([]byte(c.flagNodeSelector), &c.nodeSelector); err != nil {
			return fmt.Errorf("error decoding node selector: %w", err)
		}
	}
	if c.flagServiceAnnotations != "" {
		if err := yaml.Unmarshal([]byte(c.flagServiceAnnotations), &c.serviceAnnotations); err != nil {
			return fmt.Errorf("error decoding service annotations: %w", err)
		}
	}

	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Create managed gateway resources after installation/upgrade."
const help = `
Usage: consul-k8s-control-plane gateway-resources [options]

  Installs managed gateway class and configuration resources
	after a helm installation or upgrade in order to avoid the
	dependencies of CRDs being in-place prior to resource creation.

`

func forceClassConfig(ctx context.Context, k8sClient client.Client, o *v1alpha1.GatewayClassConfig) error {
	return backoff.Retry(func() error {
		var existing v1alpha1.GatewayClassConfig
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(o), &existing)
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}

		if k8serrors.IsNotFound(err) {
			return k8sClient.Create(ctx, o)
		}

		existing.Spec = o.Spec
		existing.Labels = o.Labels

		return k8sClient.Update(ctx, &existing)
	}, exponentialBackoffWithMaxIntervalAndTime())
}

func forceClass(ctx context.Context, k8sClient client.Client, o *gwv1beta1.GatewayClass) error {
	return backoff.Retry(func() error {
		var existing gwv1beta1.GatewayClass
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(o), &existing)
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}

		if k8serrors.IsNotFound(err) {
			return k8sClient.Create(ctx, o)
		}

		existing.Spec = o.Spec
		existing.Labels = o.Labels

		return k8sClient.Update(ctx, &existing)
	}, exponentialBackoffWithMaxIntervalAndTime())
}

func exponentialBackoffWithMaxIntervalAndTime() *backoff.ExponentialBackOff {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = 10 * time.Second
	backoff.MaxInterval = 1 * time.Second
	backoff.Reset()
	return backoff
}

func nonZeroOrNil(v int) *int32 {
	if v == 0 {
		return nil
	}
	return common.PointerTo(int32(v))
}

func serviceTypeIfSet(v string) *corev1.ServiceType {
	if v == "" {
		return nil
	}
	return common.PointerTo(corev1.ServiceType(v))
}
