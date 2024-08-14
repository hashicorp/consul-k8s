// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatewaycleanup

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/mitchellh/cli"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

const (
	gatewayConfigFilename  = "/consul/config/config.yaml"
	resourceConfigFilename = "/consul/config/resources.json"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	flagGatewayClassName           string
	flagGatewayClassConfigName     string
	flagGatewayConfigLocation      string
	flagResourceConfigFileLocation string

	k8sClient client.Client

	once sync.Once
	help string

	ctx context.Context
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagGatewayClassName, "gateway-class-name", "",
		"Name of Kubernetes GatewayClass to delete.")
	c.flags.StringVar(&c.flagGatewayClassConfigName, "gateway-class-config-name", "",
		"Name of Kubernetes GatewayClassConfig to delete.")

	c.flags.StringVar(&c.flagGatewayConfigLocation, "gateway-config-file-location", gatewayConfigFilename,
		"specify a different location for where the gateway config file is")

	c.flags.StringVar(&c.flagResourceConfigFileLocation, "resource-config-file-location", resourceConfigFilename,
		"specify a different location for where the gateway resource config file is")

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

	// Create the Kubernetes clientset
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

	// do the cleanup

	err = c.deleteGatewayClassAndGatewayClasConfig()
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	return 0
}

func (c *Command) deleteGatewayClassAndGatewayClasConfig() error {
	// find the class config and mark it for deletion first so that we
	// can do an early return if the gateway class isn't found
	config := &v1alpha1.GatewayClassConfig{}
	err := c.k8sClient.Get(context.Background(), types.NamespacedName{Name: c.flagGatewayClassConfigName}, config)
	if err != nil {

		if k8serrors.IsNotFound(err) {
			// no gateway class config, just ignore and return
			return nil
		}
		c.UI.Error(err.Error())
		return err
	}

	// ignore any returned errors
	_ = c.k8sClient.Delete(context.Background(), config)

	// find the gateway class

	gatewayClass := &gwv1beta1.GatewayClass{}
	err = c.k8sClient.Get(context.Background(), types.NamespacedName{Name: c.flagGatewayClassName}, gatewayClass)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// no gateway class, just ignore and return
			return nil
		}
		c.UI.Error(err.Error())
		return err
	}

	// ignore any returned errors
	_ = c.k8sClient.Delete(context.Background(), gatewayClass)

	// make sure they're gone
	if err := backoff.Retry(func() error {
		err = c.k8sClient.Get(context.Background(), types.NamespacedName{Name: c.flagGatewayClassConfigName}, config)
		if err == nil || !k8serrors.IsNotFound(err) {
			return errors.New("gateway class config still exists")
		}

		err = c.k8sClient.Get(context.Background(), types.NamespacedName{Name: c.flagGatewayClassName}, gatewayClass)
		if err == nil || !k8serrors.IsNotFound(err) {
			return errors.New("gateway class still exists")
		}

		return nil
	}, exponentialBackoffWithMaxIntervalAndTime()); err != nil {
		c.UI.Error(err.Error())
		// if we failed, return 0 anyway after logging the error
		// since we don't want to block someone from uninstallation
	}
	return nil
}

func (c *Command) validateFlags() error {
	if c.flagGatewayClassConfigName == "" {
		return errors.New("-gateway-class-config-name must be set")
	}
	if c.flagGatewayClassName == "" {
		return errors.New("-gateway-class-name must be set")
	}

	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Clean up global gateway resources prior to uninstall."
const help = `
Usage: consul-k8s-control-plane gateway-cleanup [options]

  Deletes installed gateway class and gateway class config objects
	prior to helm uninstallation. This is required due to finalizers
	existing on the GatewayClassConfig that will leave around a dangling
	object without deleting these prior to their controllers being deleted.
	The job is best effort, so if it fails to successfully delete the
	objects, it will allow the uninstallation to continue.

`

func exponentialBackoffWithMaxIntervalAndTime() *backoff.ExponentialBackOff {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = 10 * time.Second
	backoff.MaxInterval = 1 * time.Second
	backoff.Reset()
	return backoff
}
