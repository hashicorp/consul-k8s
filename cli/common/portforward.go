package common

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForward represents a Kubernetes Pod port forwarding session which can be
// run as a background process.
type PortForward struct {
	// Namespace is the Kubernetes Namespace where the Pod can be found.
	Namespace string
	// PodName is the name of the Pod to port forward.
	PodName string
	// RemotePort is the port on the Pod to forward to.
	RemotePort int

	// KubeClient is the Kubernetes Client to use for port forwarding.
	KubeClient kubernetes.Interface
	// KubeConfig is the Kubernetes configuration to use for port forwarding.
	KubeConfig string
	// KubeContext is the Kubernetes context to use for port forwarding.
	KubeContext string

	localPort int
	stopChan  chan struct{}
	readyChan chan struct{}
}

// Open opens a port forward session to a Kubernetes Pod.
func (pf *PortForward) Open() error {
	// Get an open port on localhost.
	if err := pf.allocateLocalPort(); err != nil {
		return fmt.Errorf("failed to allocate local port: %v", err)
	}

	// Load the Kubernetes API client configuration.
	config, err := pf.loadApiClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load API client config: %v", err)
	}

	// Configure the connection to the Pod.
	postEndpoint := pf.KubeClient.CoreV1().RESTClient().Post()
	portForwardURL := postEndpoint.
		Resource("pods").
		Namespace(pf.Namespace).
		Name(pf.PodName).
		SubResource("portforward").
		URL()

	// Create a dialer for the port forward target.
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return fmt.Errorf("failed to create roundtripper: %v", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", portForwardURL)

	// Create channels for Goroutines to communicate.
	pf.stopChan = make(chan struct{})
	pf.readyChan = make(chan struct{})
	errChan := make(chan error)

	// Create a Kubernetes port forwarder.
	ports := []string{fmt.Sprintf("%d:%d", pf.localPort, pf.RemotePort)}
	portforwarder, err := portforward.New(dialer, ports, pf.stopChan, pf.readyChan, nil, nil)
	if err != nil {
		return err
	}

	// Start port forwarding.
	go func() {
		errChan <- portforwarder.ForwardPorts()
	}()

	// Return an error from the channel if one is received, otherwise return nil
	// once the port forwarder is ready.
	select {
	case err := <-errChan:
		return err
	case <-pf.readyChan:
		return nil
	}
}

// Endpoint returns the local port that the port forwarder is listening on.
// An error is returned if the port forwarder is not running.
func (pf *PortForward) Endpoint() (string, error) {
	select {
	case <-pf.readyChan:
		return fmt.Sprintf("http://localhost:%d", pf.localPort), nil
	case <-time.After(time.Second):
		return "", fmt.Errorf("port forwarder is not running")
	}
}

// Close closes the port forward connection.
func (pf *PortForward) Close() {
	close(pf.stopChan)
}

// allocateLocalPort looks for an open port on localhost and sets it to the
// localPort field.
func (pf *PortForward) allocateLocalPort() error {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}
	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return err
	}

	pf.localPort, err = strconv.Atoi(port)
	return err
}

// loadApiClientConfig loads the Kubernetes API client configuration using the
// provided configuration file and context.
func (pf *PortForward) loadApiClientConfig() (*rest.Config, error) {
	overrides := clientcmd.ConfigOverrides{}
	if pf.KubeContext != "" {
		overrides.CurrentContext = pf.KubeContext
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: pf.KubeConfig},
		&overrides)

	return config.ClientConfig()
}
