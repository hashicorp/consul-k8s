package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/consul-k8s/cli/version"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

func main() {
	c := cli.NewCLI("consul-k8s", version.GetHumanVersion())
	c.Args = os.Args[1:]

	// Enable CLI autocomplete
	c.Autocomplete = true

	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	basecmd, commands := initializeCommands(ctx, log)
	c.Commands = commands
	defer func() {
		_ = basecmd.Close()
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		// Any cleanups, such as cancelling contexts
		cancel()
		_ = basecmd.Close()
		os.Exit(1)
	}()

	c.HelpFunc = cli.BasicHelpFunc("consul-k8s")

	exitStatus, err := c.Run()
	if err != nil {
		log.Info(err.Error())
	}
	os.Exit(exitStatus)
}
