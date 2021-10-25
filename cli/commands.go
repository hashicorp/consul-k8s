package main

import (
	"context"

	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/consul-k8s/cli/cmd/install"
	"github.com/hashicorp/consul-k8s/cli/cmd/status"
	"github.com/hashicorp/consul-k8s/cli/cmd/uninstall"
	cmdversion "github.com/hashicorp/consul-k8s/cli/cmd/version"
	"github.com/hashicorp/consul-k8s/cli/version"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

func initializeCommands(ctx context.Context, log hclog.Logger) (*common.BaseCommand, map[string]cli.CommandFactory) {

	baseCommand := &common.BaseCommand{
		Ctx: ctx,
		Log: log,
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
		"version": func() (cli.Command, error) {
			return &cmdversion.Command{
				BaseCommand: baseCommand,
				Version:     version.GetHumanVersion(),
			}, nil
		},
	}

	return baseCommand, commands
}
