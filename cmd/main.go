package main

import (
	"log"
	"os"

	"github.com/hashicorp/consul-k8s/cmd/subcommand"
	"github.com/mitchellh/cli"
)

func main() {
	c := cli.NewCLI("consul-k8s", "0.1.0")
	c.Args = os.Args[1:]
	c.Commands = map[string]cli.CommandFactory{
		"inject": func() (cli.Command, error) { return &subcommand.Inject{}, nil },
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}
	os.Exit(exitStatus)
}
