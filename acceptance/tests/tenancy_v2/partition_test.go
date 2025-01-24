// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tenancy_v2

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul/proto-public/pbresource"
	pbtenancy "github.com/hashicorp/consul/proto-public/pbtenancy/v2beta1"
)

// TestTenancy_Partition_Created tests that V2 partitions are created when requested
// by a consul client external to the consul server cluster's k8s cluster.
//
// It sets up an external Consul server in the same cluster but a different Helm installation
// and then treats this server as external.
func TestTenancy_Partition_Created(t *testing.T) {
	// Given a single k8s kind cluster
	// Where helm "server" release hosts a consul server cluster (server.enabled=true)
	// And   helm "client" release hosts a consul client cluster (server.enabled=false)
	// And   both releases have experiments "resource-apis" and "v2tenancy enabled"
	// And   helm "client" release is configured to point to the helm "server" release as an external server (externalServer.enabled=true)
	// And   helm "client" release has admin partitions enabled with name "ap1" (global.adminPartitions.name=ap1)
	// And   helm "server" release is open for business
	// When  helm "client" release is installed
	// Then  partition "ap1" is created by the partition-init job in the helm "client" release

	// We're skipping ACLs for now because they're not supported in v2.
	cfg := suite.Config()
	// Requires connnectInject.enabled which we disable below.
	cfg.SkipWhenCNI(t)
	ctx := suite.Environment().DefaultContext(t)

	serverHelmValues := map[string]string{
		"server.enabled":                 "true",
		"global.experiments[0]":          "resource-apis",
		"global.experiments[1]":          "v2tenancy",
		"global.adminPartitions.enabled": "false",
		"global.enableConsulNamespaces":  "true",

		// Don't install injector, controller and cni on this k8s cluster so that it's not installed twice.
		"connectInject.enabled": "false",

		// The UI is not supported for v2 in 1.17, so for now it must be disabled.
		"ui.enabled": "false",
	}

	serverReleaseName := helpers.RandomName()
	serverCluster := consul.NewHelmCluster(t, serverHelmValues, ctx, cfg, serverReleaseName)
	serverCluster.Create(t)

	clientHelmValues := map[string]string{
		"server.enabled":                 "false",
		"global.experiments[0]":          "resource-apis",
		"global.experiments[1]":          "v2tenancy",
		"global.adminPartitions.enabled": "true",
		"global.adminPartitions.name":    "ap1",
		"global.enableConsulNamespaces":  "true",
		"externalServers.enabled":        "true",
		"externalServers.hosts[0]":       fmt.Sprintf("%s-consul-server", serverReleaseName),

		// This needs to be set to true otherwise the pods never materialize
		"connectInject.enabled": "true",

		// The UI is not supported for v2 in 1.17, so for now it must be disabled.
		"ui.enabled": "false",
	}

	clientReleaseName := helpers.RandomName()
	clientCluster := consul.NewHelmCluster(t, clientHelmValues, ctx, cfg, clientReleaseName)
	clientCluster.SkipCheckForPreviousInstallations = true

	clientCluster.Create(t)

	// verify partition ap1 created by partition init job
	serverResourceClient := serverCluster.ResourceClient(t, false)
	_, err := serverResourceClient.Read(context.Background(), &pbresource.ReadRequest{
		Id: &pbresource.ID{
			Name: "ap1",
			Type: pbtenancy.PartitionType,
		},
	})
	require.NoError(t, err, "expected partition ap1 to be created by partition init job")
}
