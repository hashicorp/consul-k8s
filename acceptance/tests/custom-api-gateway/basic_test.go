// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package customapigateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	gw "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCustomAPIGateway_Basic(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("secure=%t", secure), func(t *testing.T) {
			h := newGatewayHarness(t, secure)
			cfg := suite.Config()
			options := h.ctx.KubectlOptions(t)
			_, _, err := h.consul.ConfigEntries().Set(&api.ProxyConfigEntry{
				Kind: api.ProxyDefaults, Name: api.ProxyConfigGlobal,
				Config: map[string]interface{}{"protocol": "http"},
			}, nil)
			require.NoError(t, err)

			k8s.DeployKustomize(t, options, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			k8s.RunKubectl(t, options, "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server")
			// Use an unmeshed client to reach the ClusterIP gateway, avoiding cloud
			// load balancers and connectivity assumptions on the test machine.
			k8s.DeployKustomize(t, options, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")
			k8s.RunKubectl(t, options, "wait", "--for=condition=available", "--timeout=5m", "deploy/static-client")
			_, class := h.createClass(t, v1alpha1.DeploymentSpec{})
			gateway := h.createGateway(t, class)
			h.checkInstances(t, gateway, 1)

			route := &gw.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: helpers.RandomName(), Namespace: h.namespace},
				Spec: gw.HTTPRouteSpec{
					CommonRouteSpec: gw.CommonRouteSpec{ParentRefs: []gw.ParentReference{{
						Group: ptr.To(gw.Group(gw.GroupVersion.Group)),
						Kind:  ptr.To(gw.Kind("Gateway")), Name: gw.ObjectName(gateway.Name),
					}}},
					Rules: []gw.HTTPRouteRule{{BackendRefs: []gw.HTTPBackendRef{{
						BackendRef: gw.BackendRef{BackendObjectReference: gw.BackendObjectReference{
							Name: "static-server", Port: ptr.To(gw.PortNumber(80)),
						}},
					}}}},
				},
			}
			h.create(t, route)
			retryGateway(t, func(r *retry.R) {
				require.NoError(r, h.k8s.Get(context.Background(), client.ObjectKeyFromObject(route), route))
				require.Len(r, route.Status.Parents, 1)
				parent := route.Status.Parents[0]
				require.EqualValues(r, controllerName, parent.ControllerName)
				require.EqualValues(r, gateway.Name, parent.ParentRef.Name)
				requireCondition(r, parent.Conditions, "Accepted", route.Generation)
				requireCondition(r, parent.Conditions, "ResolvedRefs", route.Generation)
				entry, _, err := h.consul.ConfigEntries().Get(api.HTTPRoute, route.Name, nil)
				require.NoError(r, err)
				consulRoute, ok := entry.(*api.HTTPRouteConfigEntry)
				require.True(r, ok)
				require.Len(r, consulRoute.Parents, 1)
				require.Equal(r, gateway.Name, consulRoute.Parents[0].Name)
			})

			target := fmt.Sprintf("http://%s.%s.svc:8080", gateway.Name, gateway.Namespace)
			if secure {
				k8s.CheckStaticServerHTTPConnectionFailing(t, options, "static-client", target)
				_, _, err := h.consul.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions, Name: "static-server",
					Sources: []*api.SourceIntention{{Name: gateway.Name, Action: api.IntentionActionAllow}},
				}, nil)
				require.NoError(t, err)
			}
			k8s.CheckStaticServerConnectionSuccessful(t, options, "static-client", target)
		})
	}
}
