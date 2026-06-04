// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"

	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-k8s/cli/cmd/config"
	config_read "github.com/hashicorp/consul-k8s/cli/cmd/config/read"
	gwlist "github.com/hashicorp/consul-k8s/cli/cmd/gateway/list"
	gwread "github.com/hashicorp/consul-k8s/cli/cmd/gateway/read"
	"github.com/hashicorp/consul-k8s/cli/cmd/install"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy/list"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy/loglevel"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy/read"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy/stats"
	"github.com/hashicorp/consul-k8s/cli/cmd/status"
	"github.com/hashicorp/consul-k8s/cli/cmd/troubleshoot"
	troubleshoot_proxy "github.com/hashicorp/consul-k8s/cli/cmd/troubleshoot/proxy"
	"github.com/hashicorp/consul-k8s/cli/cmd/troubleshoot/upstreams"
	"github.com/hashicorp/consul-k8s/cli/cmd/uninstall"
	"github.com/hashicorp/consul-k8s/cli/cmd/upgrade"
	cmdversion "github.com/hashicorp/consul-k8s/cli/cmd/version"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/version"
)

func initializeCommands(ctx context.Context, log hclog.Logger) (*common.BaseCommand, map[string]cli.CommandFactory) {
	baseCommand := &common.BaseCommand{
		Ctx: ctx,
		Log: log,
		UI:  terminal.NewBasicUI(ctx),
	}

	commands := map[string]cli.CommandFactory{
		"install": func() (cli.Command, error) {
			return &install.Command{
				BaseCommand: baseCommand,
			}, nil
		},
		"uninstall": func() (cli.Command, error) {
			return &uninstall.Command{
				BaseCommand: baseCommand,
			}, nil
		},
		"status": func() (cli.Command, error) {
			return &status.Command{
				BaseCommand: baseCommand,
			}, nil
		},
		"upgrade": func() (cli.Command, error) {
			return &upgrade.Command{
				BaseCommand: baseCommand,
			}, nil
		},
		"version": func() (cli.Command, error) {
			return &cmdversion.Command{
				BaseCommand: baseCommand,
				Version:     version.GetHumanVersion(),
			}, nil
		},
		"gateway list": func() (cli.Command, error) {
			return &gwlist.Command{
				BaseCommand: baseCommand,
			}, nil
		},
		"gateway read": func() (cli.Command, error) {
			return &gwread.Command{
				BaseCommand: baseCommand,
			}, nil
		},
		"proxy": func() (cli.Command, error) {
			return &proxy.ProxyCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"proxy list": func() (cli.Command, error) {
			return &list.ListCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"proxy log": func() (cli.Command, error) {
			return &loglevel.LogLevelCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"proxy read": func() (cli.Command, error) {
			return &read.ReadCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"proxy stats": func() (cli.Command, error) {
			return &stats.StatsCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"config": func() (cli.Command, error) {
			return &config.ConfigCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"config read": func() (cli.Command, error) {
			return &config_read.ReadCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"troubleshoot": func() (cli.Command, error) {
			return &troubleshoot.TroubleshootCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"troubleshoot proxy": func() (cli.Command, error) {
			return &troubleshoot_proxy.ProxyCommand{
				BaseCommand: baseCommand,
			}, nil
		},
		"troubleshoot upstreams": func() (cli.Command, error) {
			return &upstreams.UpstreamsCommand{
				BaseCommand: baseCommand,
			}, nil
		},
	}

	return baseCommand, commands
}
