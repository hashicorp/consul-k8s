package common

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
)

type mockForwarder struct {
	forwardBehavior func() error
}

func (m *mockForwarder) ForwardPorts() error                            { return m.forwardBehavior() }
func (m *mockForwarder) Close()                                         {}
func (m *mockForwarder) GetPorts() ([]portforward.ForwardedPort, error) { return nil, nil }

func TestPortForwardingSuccess(t *testing.T) {
	mockForwarder := &mockForwarder{
		forwardBehavior: func() error { return nil },
	}

	newMockForwarder := func(dialer httpstream.Dialer, ports []string, stopChan <-chan struct{}, readyChan chan struct{}, out, errOut io.Writer) (forwarder, error) {
		close(readyChan)
		return mockForwarder, nil
	}

	pf := &PortForward{
		KubeClient:     fake.NewSimpleClientset(),
		RestConfig:     &rest.Config{},
		portForwardURL: &url.URL{},
		newForwarder:   newMockForwarder,
	}

	endpoint, err := pf.Open(context.Background())
	defer pf.Close()

	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("localhost:%d", pf.localPort), endpoint)
}

func TestPortForwardingError(t *testing.T) {
	mockForwarder := &mockForwarder{
		forwardBehavior: func() error {
			return fmt.Errorf("error")
		},
	}

	newMockForwarder := func(dialer httpstream.Dialer, ports []string, stopChan <-chan struct{}, readyChan chan struct{}, out, errOut io.Writer) (forwarder, error) {
		return mockForwarder, nil
	}

	pf := &PortForward{
		KubeClient:     fake.NewSimpleClientset(),
		RestConfig:     &rest.Config{},
		portForwardURL: &url.URL{},
		newForwarder:   newMockForwarder,
	}

	endpoint, err := pf.Open(context.Background())
	defer pf.Close()

	require.Error(t, err)
	require.Equal(t, "error", err.Error())
	require.Equal(t, "", endpoint)
}

func TestPortForwardingContextCancel(t *testing.T) {
	mockForwarder := &mockForwarder{
		forwardBehavior: func() error {
			return nil
		},
	}

	newMockForwarder := func(dialer httpstream.Dialer, ports []string, stopChan <-chan struct{}, readyChan chan struct{}, out, errOut io.Writer) (forwarder, error) {
		return mockForwarder, nil
	}

	pf := &PortForward{
		KubeClient:     fake.NewSimpleClientset(),
		RestConfig:     &rest.Config{},
		portForwardURL: &url.URL{},
		newForwarder:   newMockForwarder,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	endpoint, err := pf.Open(ctx)

	require.Error(t, err)
	require.Equal(t, "port forward cancelled", err.Error())
	require.Equal(t, "", endpoint)
}

func TestPortForwardingTimeout(t *testing.T) {
	mockForwarder := &mockForwarder{
		forwardBehavior: func() error {
			time.Sleep(time.Second * 10)
			return nil
		},
	}

	newMockForwarder := func(dialer httpstream.Dialer, ports []string, stopChan <-chan struct{}, readyChan chan struct{}, out, errOut io.Writer) (forwarder, error) {
		return mockForwarder, nil
	}

	pf := &PortForward{
		KubeClient:     fake.NewSimpleClientset(),
		RestConfig:     &rest.Config{},
		portForwardURL: &url.URL{},
		newForwarder:   newMockForwarder,
	}

	endpoint, err := pf.Open(context.Background())

	require.Error(t, err)
	require.Equal(t, "port forward timed out", err.Error())
	require.Equal(t, "", endpoint)
}
