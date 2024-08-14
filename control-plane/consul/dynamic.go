// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"time"

	capi "github.com/hashicorp/consul/api"
)

type DynamicClient struct {
	ConsulClient *capi.Client
	Config       *Config
	watcher      ServerConnectionManager
}

func NewDynamicClientFromConnMgr(config *Config, watcher ServerConnectionManager) (*DynamicClient, error) {
	client, err := NewClientFromConnMgr(config, watcher)
	if err != nil {
		return nil, err
	}
	return &DynamicClient{
		ConsulClient: client,
		Config:       config,
		watcher:      watcher,
	}, nil
}

func (d *DynamicClient) RefreshClient() error {
	var err error
	var client *capi.Client
	// If the watcher is not set then we did not create the client using NewDynamicClientFromConnMgr and are using it in
	// testing
	// TODO: Use watcher in testing ;)
	if d.watcher == nil {
		return nil
	}
	client, err = NewClientFromConnMgr(d.Config, d.watcher)
	if err != nil {
		return err
	}
	d.ConsulClient = client
	return nil
}

func NewDynamicClientWithTimeout(config *capi.Config, consulAPITimeout time.Duration) (*DynamicClient, error) {
	client, err := NewClient(config, consulAPITimeout)
	if err != nil {
		return nil, err
	}
	return &DynamicClient{
		ConsulClient: client,
		Config: &Config{
			APIClientConfig: config,
		},
	}, nil
}

func NewDynamicClient(config *capi.Config) (*DynamicClient, error) {
	// defaultTimeout is taken from flags.go..
	defaultTimeout := 5 * time.Second
	client, err := NewDynamicClientWithTimeout(config, defaultTimeout)
	if err != nil {
		return nil, err
	}
	return client, nil
}
