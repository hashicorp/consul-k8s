// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatewaycleanup

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	k8syaml "sigs.k8s.io/yaml"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/gateways"
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

	gatewayConfig gateways.GatewayResources

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

		if err := v2beta1.AddMeshToScheme(s); err != nil {
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

	//V1 Cleanup
	err = c.deleteV1GatewayClassAndGatewayClasConfig()
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	//V2 Cleanup
	err = c.loadGatewayConfigs()
	if err != nil {

		c.UI.Error(err.Error())
		return 1
	}
	err = c.deleteV2GatewayClassAndClassConfigs(c.ctx)
	if err != nil {
		c.UI.Error(err.Error())

		return 1
	}

	err = c.deleteV2MeshGateways(c.ctx)
	if err != nil {
		c.UI.Error(err.Error())

		return 1
	}

	return 0
}

func (c *Command) deleteV1GatewayClassAndGatewayClasConfig() error {
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

// loadGatewayConfigs reads and loads the configs from `/consul/config/config.yaml`, if this file does not exist nothing is done.
func (c *Command) loadGatewayConfigs() error {
	file, err := os.Open(c.flagGatewayConfigLocation)
	if err != nil {
		if os.IsNotExist(err) {
			c.UI.Warn(fmt.Sprintf("gateway configuration file not found, skipping gateway configuration, filename: %s", c.flagGatewayConfigLocation))
			return nil
		}
		c.UI.Error(fmt.Sprintf("Error opening gateway configuration file %s: %s", c.flagGatewayConfigLocation, err))
		return err
	}

	config, err := io.ReadAll(file)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error reading gateway configuration file %s: %s", c.flagGatewayConfigLocation, err))
		return err
	}

	err = k8syaml.Unmarshal(config, &c.gatewayConfig)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error decoding gateway config file: %s", err))
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

func (c *Command) deleteV2GatewayClassAndClassConfigs(ctx context.Context) error {
	for _, gcc := range c.gatewayConfig.GatewayClassConfigs {

		// find the class config and mark it for deletion first so that we
		// can do an early return if the gateway class isn't found
		config := &v2beta1.GatewayClassConfig{}
		err := c.k8sClient.Get(ctx, types.NamespacedName{Name: gcc.Name, Namespace: gcc.Namespace}, config)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// no gateway class config, just ignore and continue
				continue
			}
			return err
		}

		// ignore any returned errors
		_ = c.k8sClient.Delete(context.Background(), config)

		// find the gateway class
		gatewayClass := &v2beta1.GatewayClass{}
		//TODO: NET-6838 To pull the GatewayClassName from the Configmap
		err = c.k8sClient.Get(ctx, types.NamespacedName{Name: gcc.Name, Namespace: gcc.Namespace}, gatewayClass)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// no gateway class, just ignore and continue
				continue
			}
			return err
		}

		// ignore any returned errors
		_ = c.k8sClient.Delete(context.Background(), gatewayClass)

		// make sure they're gone
		if err := backoff.Retry(func() error {
			err = c.k8sClient.Get(context.Background(), types.NamespacedName{Name: gcc.Name, Namespace: gcc.Namespace}, config)
			if err == nil || !k8serrors.IsNotFound(err) {
				return errors.New("gateway class config still exists")
			}

			err = c.k8sClient.Get(context.Background(), types.NamespacedName{Name: gcc.Name, Namespace: gcc.Namespace}, gatewayClass)
			if err == nil || !k8serrors.IsNotFound(err) {
				return errors.New("gateway class still exists")
			}

			return nil
		}, exponentialBackoffWithMaxIntervalAndTime()); err != nil {
			c.UI.Error(err.Error())
			// if we failed, return 0 anyway after logging the error
			// since we don't want to block someone from uninstallation
		}
	}

	return nil
}

func (c *Command) deleteV2MeshGateways(ctx context.Context) error {
	for _, meshGw := range c.gatewayConfig.MeshGateways {
		_ = c.k8sClient.Delete(ctx, meshGw)

		err := c.k8sClient.Get(ctx, types.NamespacedName{Name: meshGw.Name, Namespace: meshGw.Namespace}, meshGw)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// no gateway, just ignore and continue
				continue
			}
			return err
		}

	}
	return nil
}
