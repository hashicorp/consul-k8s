package controller

import (
	"github.com/hashicorp/go-hclog"
)

// TestControllerRun takes the given Resource and runs the Controller. The
// returned function should be called to stop the controller. The returned
// function will block until the controller stops.
func TestControllerRun(r Resource) func() {
	c := &Controller{Log: hclog.Default(), Resource: r}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		c.Run(stopCh)
	}()

	return func() {
		close(stopCh)
		<-doneCh
	}
}
