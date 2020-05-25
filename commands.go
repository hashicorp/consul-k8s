package main

import (
	"os"

	cmdACLInit "github.com/hashicorp/consul-k8s/subcommand/acl-init"
	cmdCreateFederationSecret "github.com/hashicorp/consul-k8s/subcommand/create-federation-secret"
	cmdDeleteCompletedJob "github.com/hashicorp/consul-k8s/subcommand/delete-completed-job"
	cmdGetConsulClientCA "github.com/hashicorp/consul-k8s/subcommand/get-consul-client-ca"
	cmdInjectConnect "github.com/hashicorp/consul-k8s/subcommand/inject-connect"
	cmdLifecycleSidecar "github.com/hashicorp/consul-k8s/subcommand/lifecycle-sidecar"
	cmdServerACLInit "github.com/hashicorp/consul-k8s/subcommand/server-acl-init"
	cmdServiceAddress "github.com/hashicorp/consul-k8s/subcommand/service-address"
	cmdSyncCatalog "github.com/hashicorp/consul-k8s/subcommand/sync-catalog"
	cmdVersion "github.com/hashicorp/consul-k8s/subcommand/version"
	"github.com/hashicorp/consul-k8s/version"
	"github.com/mitchellh/cli"
)

// Commands is the mapping of all available consul-k8s commands.
var Commands map[string]cli.CommandFactory

func init() {
	ui := &cli.BasicUi{Writer: os.Stdout, ErrorWriter: os.Stderr}

	Commands = map[string]cli.CommandFactory{
		"acl-init": func() (cli.Command, error) {
			return &cmdACLInit.Command{UI: ui}, nil
		},

		"inject-connect": func() (cli.Command, error) {
			return &cmdInjectConnect.Command{UI: ui}, nil
		},

		"lifecycle-sidecar": func() (cli.Command, error) {
			return &cmdLifecycleSidecar.Command{UI: ui}, nil
		},

		"server-acl-init": func() (cli.Command, error) {
			return &cmdServerACLInit.Command{UI: ui}, nil
		},

		"sync-catalog": func() (cli.Command, error) {
			return &cmdSyncCatalog.Command{UI: ui}, nil
		},

		"delete-completed-job": func() (cli.Command, error) {
			return &cmdDeleteCompletedJob.Command{UI: ui}, nil
		},

		"service-address": func() (cli.Command, error) {
			return &cmdServiceAddress.Command{UI: ui}, nil
		},

		"get-consul-client-ca": func() (cli.Command, error) {
			return &cmdGetConsulClientCA.Command{UI: ui}, nil
		},

		"version": func() (cli.Command, error) {
			return &cmdVersion.Command{UI: ui, Version: version.GetHumanVersion()}, nil
		},

		"create-federation-secret": func() (cli.Command, error) {
			return &cmdCreateFederationSecret.Command{UI: ui}, nil
		},
	}
}

func helpFunc() cli.HelpFunc {
	// This should be updated for any commands we want to hide for any reason.
	// Hidden commands can still be executed if you know the command, but
	// aren't shown in any help output. We use this for prerelease functionality
	// or advanced features.
	hidden := map[string]struct{}{
		"inject-connect": struct{}{},
	}

	var include []string
	for k := range Commands {
		if _, ok := hidden[k]; !ok {
			include = append(include, k)
		}
	}

	return cli.FilteredHelpFunc(include, cli.BasicHelpFunc("consul-k8s"))
}
