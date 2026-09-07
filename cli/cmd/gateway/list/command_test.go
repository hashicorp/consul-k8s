// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package read

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

func TestFlagParsing(t *testing.T) {
	cases := map[string]struct {
		args []string
		out  int
	}{
		"No args": {
			args: []string{},
			out:  1,
		},
		"Multiple gateway names passed": {
			args: []string{"gateway-1", "gateway-2"},
			out:  1,
		},
		"Nonexistent flag passed, -foo bar": {
			args: []string{"gateway-1", "-foo", "bar"},
			out:  1,
		},
		"Invalid argument passed, -namespace YOLO": {
			args: []string{"gateway-1", "-namespace", "YOLO"},
			out:  1,
		},
		"User passed incorrect output": {
			args: []string{"gateway-1", "-output", "image"},
			out:  1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := setupCommand(new(bytes.Buffer))
			c.kubernetes = fake.NewClientBuilder().WithObjectTracker(nil).Build()

			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

func TestReadCommandOutput(t *testing.T) {
	gatewayClassName := "gateway-class-1"
	gatewayName := "gateway-1"
	routeName := "route-1"

	fakeGatewayClass := &gwv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
		},
	}

	fakeGateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      gatewayName,
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: gwv1.ObjectName(gatewayClassName),
		},
	}

	fakeHTTPRoute := &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      routeName,
		},
		Spec: gwv1.HTTPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{
				ParentRefs: []gwv1.ParentReference{
					{
						Name: gwv1.ObjectName(fakeGateway.Name),
					},
				},
			},
		},
	}

	fakeUnattachedHTTPRoute := &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "route-2",
		},
	}

	buf := new(bytes.Buffer)
	c := setupCommand(buf)

	scheme := scheme.Scheme
	gwv1.Install(scheme)
	gwv1alpha2.Install(scheme)

	c.kubernetes = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(fakeGatewayClass, fakeGateway, fakeHTTPRoute, fakeUnattachedHTTPRoute).
		Build()

	out := c.Run([]string{"-output", "json"})
	require.Equal(t, 0, out)

	gatewaysWithRoutes := []struct {
		Gateway      gwv1.Gateway      `json:"Gateway"`
		GatewayClass gwv1.GatewayClass `json:"GatewayClass"`
		HTTPRoutes   []gwv1.HTTPRoute  `json:"HTTPRoutes"`
	}{}
	require.NoErrorf(t, json.Unmarshal(buf.Bytes(), &gatewaysWithRoutes), "failed to parse JSON output %s", buf.String())
	require.Len(t, gatewaysWithRoutes, 1)

	gatewayWithRoutes := gatewaysWithRoutes[0]

	// Make gateway assertions
	assert.Equal(t, gatewayName, gatewayWithRoutes.Gateway.Name)

	// Make gateway class assertions
	assert.Equal(t, gatewayClassName, gatewayWithRoutes.GatewayClass.Name)

	// Make http route assertions
	require.Len(t, gatewayWithRoutes.HTTPRoutes, 1)
	assert.Equal(t, routeName, gatewayWithRoutes.HTTPRoutes[0].Name)
}

func setupCommand(buf io.Writer) *Command {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	// Setup and initialize the command struct
	command := &Command{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()

	return command
}
