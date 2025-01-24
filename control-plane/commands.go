// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"

	"github.com/mitchellh/cli"

	cmdACLInit "github.com/hashicorp/consul-k8s/control-plane/subcommand/acl-init"
	cmdConnectInit "github.com/hashicorp/consul-k8s/control-plane/subcommand/connect-init"
	cmdConsulLogout "github.com/hashicorp/consul-k8s/control-plane/subcommand/consul-logout"
	cmdCreateFederationSecret "github.com/hashicorp/consul-k8s/control-plane/subcommand/create-federation-secret"
	cmdDeleteCompletedJob "github.com/hashicorp/consul-k8s/control-plane/subcommand/delete-completed-job"
	cmdFetchServerRegion "github.com/hashicorp/consul-k8s/control-plane/subcommand/fetch-server-region"
	cmdGatewayCleanup "github.com/hashicorp/consul-k8s/control-plane/subcommand/gateway-cleanup"
	cmdGatewayResources "github.com/hashicorp/consul-k8s/control-plane/subcommand/gateway-resources"
	cmdGetConsulClientCA "github.com/hashicorp/consul-k8s/control-plane/subcommand/get-consul-client-ca"
	cmdGossipEncryptionAutogenerate "github.com/hashicorp/consul-k8s/control-plane/subcommand/gossip-encryption-autogenerate"
	cmdInjectConnect "github.com/hashicorp/consul-k8s/control-plane/subcommand/inject-connect"
	cmdInstallCNI "github.com/hashicorp/consul-k8s/control-plane/subcommand/install-cni"
	cmdMeshInit "github.com/hashicorp/consul-k8s/control-plane/subcommand/mesh-init"
	cmdPartitionInit "github.com/hashicorp/consul-k8s/control-plane/subcommand/partition-init"
	cmdServerACLInit "github.com/hashicorp/consul-k8s/control-plane/subcommand/server-acl-init"
	cmdSyncCatalog "github.com/hashicorp/consul-k8s/control-plane/subcommand/sync-catalog"
	cmdTLSInit "github.com/hashicorp/consul-k8s/control-plane/subcommand/tls-init"
	cmdVersion "github.com/hashicorp/consul-k8s/control-plane/subcommand/version"
	webhookCertManager "github.com/hashicorp/consul-k8s/control-plane/subcommand/webhook-cert-manager"
	"github.com/hashicorp/consul-k8s/version"
)

// Commands is the mapping of all available consul-k8s commands.
var Commands map[string]cli.CommandFactory

func init() {
	ui := &cli.BasicUi{Writer: os.Stdout, ErrorWriter: os.Stderr}

	Commands = map[string]cli.CommandFactory{
		"acl-init": func() (cli.Command, error) {
			return &cmdACLInit.Command{UI: ui}, nil
		},

		"connect-init": func() (cli.Command, error) {
			return &cmdConnectInit.Command{UI: ui}, nil
		},

		"mesh-init": func() (cli.Command, error) {
			return &cmdMeshInit.Command{UI: ui}, nil
		},

		"inject-connect": func() (cli.Command, error) {
			return &cmdInjectConnect.Command{UI: ui}, nil
		},

		"consul-logout": func() (cli.Command, error) {
			return &cmdConsulLogout.Command{UI: ui}, nil
		},

		"gateway-cleanup": func() (cli.Command, error) {
			return &cmdGatewayCleanup.Command{UI: ui}, nil
		},

		"gateway-resources": func() (cli.Command, error) {
			return &cmdGatewayResources.Command{UI: ui}, nil
		},

		"server-acl-init": func() (cli.Command, error) {
			return &cmdServerACLInit.Command{UI: ui}, nil
		},

		"partition-init": func() (cli.Command, error) {
			return &cmdPartitionInit.Command{UI: ui}, nil
		},

		"sync-catalog": func() (cli.Command, error) {
			return &cmdSyncCatalog.Command{UI: ui}, nil
		},

		"delete-completed-job": func() (cli.Command, error) {
			return &cmdDeleteCompletedJob.Command{UI: ui}, nil
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

		"webhook-cert-manager": func() (cli.Command, error) {
			return &webhookCertManager.Command{UI: ui}, nil
		},

		"tls-init": func() (cli.Command, error) {
			return &cmdTLSInit.Command{UI: ui}, nil
		},

		"gossip-encryption-autogenerate": func() (cli.Command, error) {
			return &cmdGossipEncryptionAutogenerate.Command{UI: ui}, nil
		},
		"install-cni": func() (cli.Command, error) {
			return &cmdInstallCNI.Command{UI: ui}, nil
		},
		"fetch-server-region": func() (cli.Command, error) {
			return &cmdFetchServerRegion.Command{UI: ui}, nil
		},
	}
}

func helpFunc() cli.HelpFunc {
	// This should be updated for any commands we want to hide for any reason.
	// Hidden commands can still be executed if you know the command, but
	// aren't shown in any help output. We use this for prerelease functionality
	// or advanced features.
	hidden := map[string]struct{}{
		"inject-connect": {},
	}

	var include []string
	for k := range Commands {
		if _, ok := hidden[k]; !ok {
			include = append(include, k)
		}
	}

	return cli.FilteredHelpFunc(include, cli.BasicHelpFunc("consul-k8s"))
}
