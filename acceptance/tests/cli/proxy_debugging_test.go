package cli

import "testing"

func TestProxyDebugging(t *testing.T) {
	// Install Consul
	// Install static client and static server
	// Use proxy list to get the name of static client pod
	// Test that proxy read gets the correct output from static client
	// Create an intention blocking traffic from server to client
	// Test that the proxy read output is updated showing this denial
}
