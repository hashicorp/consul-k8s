// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package mesh_v2

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

const multiport = "multiport"

// Test that mesh sidecar proxies work for an application with multiple ports. The multiport application is a Pod listening on
// two ports. This tests inbound connections to each port of the multiport app, and outbound connections from the
// multiport app to static-server.
func TestMeshInject_MultiportService(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)

		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			cfg.SkipWhenOpenshiftAndCNI(t)
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"global.experiments[0]": "resource-apis",
				// The UI is not supported for v2 in 1.17, so for now it must be disabled.
				"ui.enabled":            "false",
				"connectInject.enabled": "true",
				// Enable DNS so we can test that DNS redirection _isn't_ set in the pod.
				"dns.enabled": "true",

				"global.tls.enabled":           strconv.FormatBool(secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient, _ := consulCluster.SetupConsulClient(t, secure)

			// Check that the ACL token is deleted.
			if secure {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					retrier := &retry.Timer{Timeout: 5 * time.Minute, Wait: 1 * time.Second}
					retry.RunWith(retrier, t, func(r *retry.R) {
						tokens, _, err := consulClient.ACL().TokenList(nil)
						require.NoError(r, err)
						for _, token := range tokens {
							require.NotContains(r, token.Description, multiport)
							require.NotContains(r, token.Description, connhelper.StaticClientName)
						}
					})
				})
			}

			logger.Log(t, "creating multiport static-server and static-client deployments")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/bases/v2-multiport-app")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/cases/v2-static-client-inject-tproxy")
			} else {
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/cases/v2-static-client-inject")
			}

			// Check that static-client has been injected and now has 2 containers.
			podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=static-client",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)

			// Check that multiport has been injected and now has 3 containers.
			podList, err = ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=multiport",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 3)

			if !secure {
				k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../../tests/fixtures/cases/trafficpermissions-deny")
			}

			// Now test that traffic is denied between the source and the destination.
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://multiport:8080")
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://multiport:9090")
			} else {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:2345")
			}
			k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../../tests/fixtures/bases/trafficpermissions")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../../tests/fixtures/bases/trafficpermissions")
			})

			// TODO: add a trafficpermission to a particular port and validate

			// Check connection from static-client to multiport.
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://multiport:8080")
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:1234")
			}

			// Check connection from static-client to multiport-admin.
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "hello world from 9090 admin", "http://multiport:9090")
			} else {
				k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "hello world from 9090 admin", "http://localhost:2345")
			}

			// Test that kubernetes readiness status is synced to Consul. This will make the multi port pods unhealthy
			// and check inbound connections to the multi port pods' services.
			// Create the files so that the readiness probes of the multi port pod fails.
			logger.Log(t, "testing k8s -> consul health checks sync by making the multiport unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+multiport, "-c", "multiport", "--", "touch", "/tmp/unhealthy-multiport")
			logger.Log(t, "testing k8s -> consul health checks sync by making the multiport-admin unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+multiport, "-c", "multiport-admin", "--", "touch", "/tmp/unhealthy-multiport-admin")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://multiport:8080")
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://multiport:9090")
			} else {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:2345")
			}
		})
	}
}
