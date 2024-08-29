// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"log"
	"os"

	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-k8s/version"
)

func main() {
	c := cli.NewCLI("consul-k8s", version.GetHumanVersion())
	c.Args = os.Args[1:]
	c.Commands = Commands
	c.HelpFunc = helpFunc()

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}
	os.Exit(exitStatus)
}
