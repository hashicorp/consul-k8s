package main

import (
	"context"

	"github.com/hashicorp/consul-k8s/cli/cmd/install"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy/list"
	"github.com/hashicorp/consul-k8s/cli/cmd/proxy/read"
	"github.com/hashicorp/consul-k8s/cli/cmd/status"
	"github.com/hashicorp/consul-k8s/cli/cmd/uninstall"
	"github.com/hashicorp/consul-k8s/cli/cmd/upgrade"
	cmdversion "github.com/hashicorp/consul-k8s/cli/cmd/version"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/version"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
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
		"proxy read": func() (cli.Command, error) {
			return &read.ReadCommand{
				BaseCommand: baseCommand,
			}, nil
		},
	}

	return baseCommand, commands
}
