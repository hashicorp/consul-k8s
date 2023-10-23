// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package mesh_v2

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// TestMeshInject_MultiportService asserts that mesh sidecar proxies work for an application with multiple ports.
// The multiport application is a Pod listening on two ports. This tests inbound connections to each port of the
// multiport app, and outbound connections from the multiport app to static-server.
func TestMeshInject_MultiportService(t *testing.T) {
	sourceOne := connhelper.StaticClientName
	sourceTwo := fmt.Sprintf("%s-2", sourceOne)
	sources := []string{sourceOne, sourceTwo}
	const destinationOne = "multiport"
	destinationTwo := fmt.Sprintf("%s-2", destinationOne)
	explicitDestinations := []string{
		"http://localhost:1234",
		"http://localhost:2345",
		"http://localhost:4321",
		"http://localhost:5432",
	}
	implicitDestinations := []string{
		fmt.Sprintf("http://%s:8080", destinationOne),
		fmt.Sprintf("http://%s:9090", destinationOne),
		fmt.Sprintf("http://%s:8080", destinationTwo),
		fmt.Sprintf("http://%s:9090", destinationTwo),
	}
	resetConnectionErrs := []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}

	for _, secure := range []bool{false} {
		name := fmt.Sprintf("secure: %t", secure)

		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			cfg.SkipWhenOpenshiftAndCNI(t)
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"global.image":                "docker.mirror.hashicorp.services/hashicorppreview/consul:1.17.0",
				"global.imageK8S":             "docker.mirror.hashicorp.services/hashicorppreview/consul-k8s-control-plane:1.3.0",
				"global.imageConsulDataplane": "docker.mirror.hashicorp.services/hashicorppreview/consul-dataplane:1.3.0",
				"global.experiments[0]":       "resource-apis",
				// The UI is not supported for v2 in 1.17, so for now it must be disabled.
				"ui.enabled":            "false",
				"connectInject.enabled": "true",
				// Enable DNS so we can test that DNS redirection _isn't_ set in the pod.
				"dns.enabled": "true",

				"global.tls.enabled":                   strconv.FormatBool(secure),
				"global.acls.manageSystemACLs":         strconv.FormatBool(secure),
				"global.gossipEncryption.autoGenerate": strconv.FormatBool(secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating 2 sources and 2 destinations")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/bases/v2-multiport-app")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/bases/v2-multiport-app-2")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/cases/v2-static-client-inject-tproxy")
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/cases/v2-static-client-inject-2-tproxy")
			} else {
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/cases/v2-static-client-inject")
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../../tests/fixtures/cases/v2-static-client-inject-2")
			}

			// Check that sources has been injected and now has 2 containers.
			k8s.CheckPods(t, ctx, fmt.Sprintf("app=%s", sourceOne), 1, 2)
			k8s.CheckPods(t, ctx, fmt.Sprintf("app=%s", sourceTwo), 1, 2)

			// Check that destinations has been injected and now has 3 containers.
			k8s.CheckPods(t, ctx, fmt.Sprintf("app=%s", destinationOne), 1, 3)
			k8s.CheckPods(t, ctx, fmt.Sprintf("app=%s", destinationTwo), 1, 3)

			if !secure {
				k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../../tests/fixtures/cases/trafficpermissions-deny")
			}

			// Now test that traffic is denied between the source and the destination.
			if cfg.EnableTransparentProxy {
				k8s.CheckSourceToDestinationCommunication(t, ctx, sources, implicitDestinations, resetConnectionErrs)
			} else {
				k8s.CheckSourceToDestinationCommunication(t, ctx, sources, explicitDestinations, resetConnectionErrs)
			}

			// Enable traffic permissions so two sources can dial two destinations.
			k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../../tests/fixtures/bases/trafficpermissions")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../../tests/fixtures/bases/trafficpermissions")
			})

			// TODO: add a trafficpermission to a particular port and validate

			// Check connection from sources to destinations.
			if cfg.EnableTransparentProxy {
				k8s.CheckSourceToDestinationCommunication(t, ctx, sources, implicitDestinations, nil)
			} else {
				k8s.CheckSourceToDestinationCommunication(t, ctx, sources, explicitDestinations, nil)
			}

			// Test that kubernetes readiness status is synced to Consul. This will make the multi port pods unhealthy
			// and check inbound connections to the multi port pods' services.
			// Create the files so that the readiness probes of the multi port pod fails.
			destinations := []string{destinationOne, destinationTwo}
			for _, dst := range destinations {
				logger.Logf(t, "testing k8s -> consul health checks sync by making the %s unhealthy", dst)
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+dst, "-c", dst, "--", "touch", "/tmp/unhealthy-"+dst)
				logger.Logf(t, "testing k8s -> consul health checks sync by making the %s unhealthy", dst+"-admin")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+dst, "-c", dst+"-admin", "--", "touch", "/tmp/unhealthy-"+dst+"-admin")
			}

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			if cfg.EnableTransparentProxy {
				k8s.CheckSourceToDestinationCommunication(t, ctx, sources, implicitDestinations, resetConnectionErrs)
			} else {
				k8s.CheckSourceToDestinationCommunication(t, ctx, sources, explicitDestinations, resetConnectionErrs)
			}

			// TODO: verify that ACL tokens are removed
		})
	}
}
