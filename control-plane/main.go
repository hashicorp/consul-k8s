package main

import (
	"log"
	"os"

	"github.com/hashicorp/consul-k8s/control-plane/version"
	"github.com/mitchellh/cli"
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
