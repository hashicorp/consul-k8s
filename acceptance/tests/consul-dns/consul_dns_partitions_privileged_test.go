// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// TestConsulDNSProxy_WithPartitionsAndCatalogSync verifies DNS queries for services across partitions
// when DNS proxy is enabled. It configures CoreDNS to use configure consul domain queries to
// be forwarded to the Consul DNS Proxy.  The test validates:
// - returning the local partition's service when tenancy is not included in the DNS question.
// - properly not resolving DNS for unexported services when ACLs are enabled.
// - properly resolving DNS for exported services when ACLs are enabled.
func TestConsulDNSProxy_WithPartitionsAndCatalogSyncPrivileged(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set")
	}
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []dnsWithPartitionsTestCase{
		{
			name:   "not secure - ACLs and auto-encrypt not enabled",
			secure: false,
		},
		{
			name:   "secure - ACLs and auto-encrypt enabled",
			secure: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defaultClusterContext := env.DefaultContext(t)
			secondaryClusterContext := env.Context(t, 1)

			// Setup the clusters and the static service.
			releaseName, consulClient, defaultPartitionOpts, secondaryPartitionQueryOpts, defaultConsulCluster := setupClustersAndStaticService(t, cfg,
				defaultClusterContext, secondaryClusterContext, c, secondaryPartition,
				defaultPartition, "53")

			// Update CoreDNS to use the Consul domain and forward queries to the Consul DNS Service or Proxy.
			updateCoreDNSWithConsulDomain_Privileged(t, defaultClusterContext, releaseName, true)
			updateCoreDNSWithConsulDomain_Privileged(t, secondaryClusterContext, releaseName, true)

			// Validate DNS proxy privileged port configuration.
			validateDNSProxyPrivilegedPort(t, defaultClusterContext, releaseName)
			validateDNSProxyPrivilegedPort(t, secondaryClusterContext, releaseName)

			podLabelSelector := "app=static-server"
			// The index of the dnsUtils pod to use for the DNS queries so that the pod name can be unique.
			dnsUtilsPodIndex := 0

			// When ACLs are enabled, the unexported service should not resolve.
			shouldResolveUnexportedCrossPartitionDNSRecord := true
			if c.secure {
				shouldResolveUnexportedCrossPartitionDNSRecord = false
			}

			// Verify that the service is in the catalog under each partition.
			verifyServiceInCatalog(t, consulClient, defaultPartitionOpts)
			verifyServiceInCatalog(t, consulClient, secondaryPartitionQueryOpts)

			logger.Log(t, "verify the service via DNS in the default partition of the Consul catalog.")
			for _, v := range getVerifications(defaultClusterContext, secondaryClusterContext,
				shouldResolveUnexportedCrossPartitionDNSRecord, cfg, releaseName, defaultConsulCluster) {
				t.Run(v.name, func(t *testing.T) {
					if v.preProcessingFunc != nil {
						v.preProcessingFunc(t)
					}
					verifyDNS(t, cfg, releaseName, staticServerNamespace, v.requestingCtx, v.svcContext,
						podLabelSelector, v.svcName, v.shouldResolveDNS, dnsUtilsPodIndex)
					dnsUtilsPodIndex++
				})
			}
		})
	}
}
