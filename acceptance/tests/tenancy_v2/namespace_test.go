// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tenancy_v2

import (
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/resource"
	"github.com/hashicorp/consul/proto-public/pbresource"
	pbtenancy "github.com/hashicorp/consul/proto-public/pbtenancy/v2beta1"
)

// TestTenancy_Namespace_Mirrored tests consul namespaces are created/deleted
// to mirror k8s namespaces in the default partition.
func TestTenancy_Namespace_Mirrored(t *testing.T) {
	cfg := suite.Config()
	cfg.SkipWhenCNI(t)
	ctx := suite.Environment().DefaultContext(t)

	serverHelmValues := map[string]string{
		"server.enabled":        "true",
		"global.experiments[0]": "resource-apis",
		"global.experiments[1]": "v2tenancy",
		// The UI is not supported for v2 in 1.17, so for now it must be disabled.
		"ui.enabled": "false",
	}

	serverReleaseName := helpers.RandomName()
	serverCluster := consul.NewHelmCluster(t, serverHelmValues, ctx, cfg, serverReleaseName)
	serverCluster.Create(t)

	logger.Log(t, "creating namespace ns1 in k8s")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "namespace", "ns1")

	logger.Log(t, "waiting for namespace ns1 to be created in consul")
	serverResourceClient := serverCluster.ResourceClient(t, false)
	rtest := resource.NewResourceTester(serverResourceClient)
	rtest.WaitForResourceExists(t, &pbresource.ID{
		Name: "ns1",
		Type: pbtenancy.NamespaceType,
		Tenancy: &pbresource.Tenancy{
			Partition: "default",
		},
	})

	logger.Log(t, "deleting namespace ns1 in k8s")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "namespace", "ns1")

	logger.Log(t, "waiting for namespace ns1 to be deleted in consul")
	rtest.WaitForResourceNotFound(t, &pbresource.ID{
		Name: "ns1",
		Type: pbtenancy.NamespaceType,
		Tenancy: &pbresource.Tenancy{
			Partition: "default",
		},
	})
}
