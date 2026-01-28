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

	// Below channel is required to handle cleanup on signal interrupt
	// - By default, main assumes that no command requires cleanup, so it sends false to the channel
	// - If a command requires cleanup,
	// 	1. It should read from this channel BEFORE context cancellation,
	// 		so channel will be empty and "signal handler goroutine" in main will wait unitl
	// 		command sends true to this channel.
	//  2. Command will/should sends true to the channel only AFTER cleanup is completed,
	// 		so that the signal handler goroutine in main can proceed to exit.
	cleanupReqAndCompleted := make(chan bool, 1)
	cleanupReqAndCompleted <- false // by default no cleanup required

	basecmd, commands := initializeCommands(ctx, log, cleanupReqAndCompleted)
	c.Commands = commands
	defer func() {
		_ = basecmd.Close()
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	// Signal handler goroutine
	go func() {
		<-ch
		// Any cleanups, such as cancelling contexts
		cancel()
		<-cleanupReqAndCompleted // by default this will be false, so this will proceed to exit, but if a command requires cleanup, it will wait here(empty channel)
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
