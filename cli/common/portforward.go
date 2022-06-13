package common

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/util/httpstream"
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

	restConfig     *rest.Config
	portForwardURL *url.URL
	newForwarder   func(httpstream.Dialer, []string, <-chan struct{}, chan struct{}, io.Writer, io.Writer) (forwarder, error)
}

// PortForwarder enables a user to open and close a connection to a remote server.
type PortForwarder interface {
	Open(context.Context) (string, error)
	Close()
}

// forwarder is an interface which can be used for opening a port forward session.
type forwarder interface {
	ForwardPorts() error
	Close()
	GetPorts() ([]portforward.ForwardedPort, error)
}

// Open opens a port forward session to a Kubernetes Pod.
func (pf *PortForward) Open(ctx context.Context) (string, error) {
	// Get an open port on localhost.
	if err := pf.allocateLocalPort(); err != nil {
		return "", fmt.Errorf("failed to allocate local port: %v", err)
	}

	// Load the Kubernetes REST client configuration.
	if err := pf.loadRestConfig(); err != nil {
		return "", fmt.Errorf("failed to load REST client configuration: %v", err)
	}

	// Configure the URL for starting the port forward.
	if pf.portForwardURL == nil {
		pf.portForwardURL = pf.KubeClient.CoreV1().RESTClient().Post().Resource("pods").Namespace(pf.Namespace).
			Name(pf.PodName).SubResource("portforward").URL()
	}

	// Create a dialer for the port forward target.
	transport, upgrader, err := spdy.RoundTripperFor(pf.restConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create roundtripper: %v", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", pf.portForwardURL)

	// Create channels for Goroutines to communicate.
	pf.stopChan = make(chan struct{}, 1)
	pf.readyChan = make(chan struct{}, 1)
	errChan := make(chan error)

	// Use the default Kubernetes port forwarder if none is specified.
	if pf.newForwarder == nil {
		pf.newForwarder = newDefaultForwarder
	}

	// Create a Kubernetes port forwarder.
	ports := []string{fmt.Sprintf("%d:%d", pf.localPort, pf.RemotePort)}
	portforwarder, err := pf.newForwarder(dialer, ports, pf.stopChan, pf.readyChan, nil, nil)
	if err != nil {
		return "", err
	}

	// Start port forwarding.
	go func() {
		errChan <- portforwarder.ForwardPorts()
	}()

	select {
	case <-pf.readyChan:
		return fmt.Sprintf("localhost:%d", pf.localPort), nil
	case err := <-errChan:
		return "", err
	case <-ctx.Done():
		pf.Close()
		return "", fmt.Errorf("port forward cancelled")
	case <-time.After(time.Second * 5):
		pf.Close()
		return "", fmt.Errorf("port forward timed out")
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

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return err
	}

	if err := listener.Close(); err != nil {
		return fmt.Errorf("unable to close listener %v", err)
	}

	pf.localPort, err = strconv.Atoi(port)
	return err
}

// loadRestConfig loads the Kubernetes REST client configuration using the
// provided Kubernetes configuration file and context.
func (pf *PortForward) loadRestConfig() (err error) {
	overrides := clientcmd.ConfigOverrides{}
	if pf.KubeContext != "" {
		overrides.CurrentContext = pf.KubeContext
	}

	if pf.restConfig == nil {
		pf.restConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: pf.KubeConfig},
			&overrides).ClientConfig()
		if err != nil {
			return err
		}
	}

	return nil
}

// newDefaultForwarder creates a new Kubernetes port forwarder.
func newDefaultForwarder(dialer httpstream.Dialer, ports []string, stopChan <-chan struct{}, readyChan chan struct{}, out, errOut io.Writer) (forwarder, error) {
	return portforward.New(dialer, ports, stopChan, readyChan, out, errOut)
}
