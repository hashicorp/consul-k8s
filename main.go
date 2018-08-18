package main

import (
	"log"
	"os"

	"github.com/hashicorp/consul-k8s/subcommand"
	"github.com/mitchellh/cli"
)

func main() {
	ui := &cli.BasicUi{Writer: os.Stdout, ErrorWriter: os.Stderr}
	c := cli.NewCLI("consul-k8s", "0.1.0")
	c.Args = os.Args[1:]
	c.Commands = map[string]cli.CommandFactory{
		"inject": func() (cli.Command, error) { return &subcommand.Inject{UI: ui}, nil },
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}
	os.Exit(exitStatus)
}
