// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package portforward

import (
	"fmt"
	"net"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// CreateTunnelToResourcePort returns a local address:port that is tunneled to the given resource's port.
func CreateTunnelToResourcePort(t *testing.T, resourceName string, remotePort int, options *terratestk8s.KubectlOptions, logger terratestLogger.TestLogger) string {
	localPort := terratestk8s.GetAvailablePort(t)
	tunnel := terratestk8s.NewTunnelWithLogger(
		options,
		terratestk8s.ResourceTypePod,
		resourceName,
		localPort,
		remotePort,
		logger)

	// Retry creating the port forward since it can fail occasionally.
	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
		// NOTE: It's okay to pass in `t` to ForwardPortE despite being in a retry
		// because we're using ForwardPortE (not ForwardPort) so the `t` won't
		// get used to fail the test, just for logging.
		require.NoError(r, tunnel.ForwardPortE(r))
	})

	doneChan := make(chan bool)

	t.Cleanup(func() {
		close(doneChan)
	})

	go monitorPortForwardedServer(t, localPort, tunnel, doneChan, resourceName, remotePort, options, logger)

	return fmt.Sprintf("127.0.0.1:%d", localPort)
}

func monitorPortForwardedServer(t *testing.T, port int, tunnel *terratestk8s.Tunnel, doneChan chan bool, resourceName string, remotePort int, options *terratestk8s.KubectlOptions, log terratestLogger.TestLogger) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-doneChan:
			logger.Log(t, "stopping monitor of the port-forwarded server")
			tunnel.Close()
			return
		case <-ticker.C:
			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				logger.Log(t, "lost connection to port-forwarded server; restarting port-forwarding", "port", port)
				tunnel.Close()
				tunnel = terratestk8s.NewTunnelWithLogger(
					options,
					terratestk8s.ResourceTypePod,
					resourceName,
					port,
					remotePort,
					log)
				err = tunnel.ForwardPortE(t)
				if err != nil {
					// If we couldn't establish a port forwarding channel, continue, so we can try again.
					continue
				}
			}
			if conn != nil {
				// Ignore error because we don't care if connection is closed successfully or not.
				_ = conn.Close()
			}
		}
	}
}
