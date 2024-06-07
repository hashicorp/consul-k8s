// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/version"
)

//go:generate mockery --name ServerConnectionManager --inpkg
type ServerConnectionManager interface {
	State() (discovery.State, error)
	Run()
	Stop()
}

// NewClient returns a V1 Consul API client. It adds a required User-Agent
// header that describes the version of consul-k8s making the call.
func NewClient(config *capi.Config, consulAPITimeout time.Duration) (*capi.Client, error) {
	if consulAPITimeout <= 0 {
		// This is only here as a last resort scenario.  This should not get
		// triggered because all components should pass the value.
		consulAPITimeout = 5 * time.Second
	}
	if config.HttpClient == nil {
		config.HttpClient = &http.Client{
			Timeout: consulAPITimeout,
		}
	}

	if config.Transport == nil {
		tlsClientConfig, err := capi.SetupTLSConfig(&config.TLSConfig)

		if err != nil {
			return nil, err
		}

		config.Transport = &http.Transport{TLSClientConfig: tlsClientConfig}
	} else if config.Transport.TLSClientConfig == nil {
		tlsClientConfig, err := capi.SetupTLSConfig(&config.TLSConfig)

		if err != nil {
			return nil, err
		}

		config.Transport.TLSClientConfig = tlsClientConfig
	}
	config.HttpClient.Transport = config.Transport

	client, err := capi.NewClient(config)
	if err != nil {
		return nil, err
	}
	client.AddHeader("User-Agent", fmt.Sprintf("consul-k8s/%s", version.GetHumanVersion()))
	return client, nil
}

type Config struct {
	APIClientConfig *capi.Config
	HTTPPort        int
	GRPCPort        int
	APITimeout      time.Duration
}

// todo (ishustava): replace all usages of this one.
// NewClientFromConnMgrState creates a new V1 API client with an IP address from the state
// of the consul-server-connection-manager.
func NewClientFromConnMgrState(config *Config, state discovery.State) (*capi.Client, error) {
	ipAddress := state.Address.IP
	config.APIClientConfig.Address = fmt.Sprintf("%s:%d", ipAddress.String(), config.HTTPPort)
	if state.Token != "" {
		config.APIClientConfig.Token = state.Token
	}
	return NewClient(config.APIClientConfig, config.APITimeout)
}

// NewClientFromConnMgr creates a new V1 API client by first getting the state of the passed watcher.
func NewClientFromConnMgr(config *Config, watcher ServerConnectionManager) (*capi.Client, error) {
	// Create a new consul client.
	serverState, err := watcher.State()
	if err != nil {
		return nil, err
	}
	consulClient, err := NewClientFromConnMgrState(config, serverState)
	if err != nil {
		return nil, err
	}
	return consulClient, nil
}
