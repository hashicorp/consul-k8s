// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/consul-k8s/version"
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

	// Below two channels are to handle graceful cleanup when terminal catches a signal interrupt/terminate
	// CleanupReq channel - Signals whether a command needs cleanup (true/false), needed to block main until cleanup is done
	// CleanupConfirmation channel - Signals when cleanup is complete (sends 1) to unblock main
	cleanupConfirmation := make(chan int, 1)
	cleanupReq := make(chan bool, 1)
	cleanupReq <- false // by default no cleanup required

	basecmd, commands := initializeCommands(ctx, log, cleanupConfirmation, cleanupReq)
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
		if cleanup := <-cleanupReq; cleanup {
			<-cleanupConfirmation
		}
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
