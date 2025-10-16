// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
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

			logger.Log(t, "Both primary and secondary consul clusters are using DNS proxy with privileged port 53")

			podLabelSelector := "app=static-server"
			// The index of the dnsUtils pod to use for the DNS queries so that the pod name can be unique.
			dnsUtilsPodIndex := 0

			// When ACLs are enabled, the unexported service should not resolve.
			shouldResolveUnexportedCrossPartitionDNSRecord := true
			if c.secure {
				shouldResolveUnexportedCrossPartitionDNSRecord = false
			}

			logger.Log(t, "Secure mode", "enabled", c.secure)

			// Verify that the service is in the catalog under each partition.
			verifyServiceInCatalog(t, consulClient, defaultPartitionOpts)
			verifyServiceInCatalog(t, consulClient, secondaryPartitionQueryOpts)

			logger.Log(t, "verify the service via DNS in the default partition of the Consul catalog.")
			for _, v := range getVerificationsPrivileged(defaultClusterContext, secondaryClusterContext,
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

func getVerificationsPrivileged(defaultClusterContext environment.TestContext, secondaryClusterContext environment.TestContext,
	shouldResolveUnexportedCrossPartitionDNSRecord bool, cfg *config.TestConfig, releaseName string, defaultConsulCluster *consul.HelmCluster) []dnsVerification {
	serviceRequestWithNoPartition := fmt.Sprintf("%s.service.consul", staticServerName)
	serviceRequestInDefaultPartition := fmt.Sprintf("%s.service.%s.ap.consul", staticServerName, defaultPartition)
	serviceRequestInSecondaryPartition := fmt.Sprintf("%s.service.%s.ap.consul", staticServerName, secondaryPartition)
	verifications := []dnsVerification{
		{
			name:             "verify static-server.service.consul from default partition resolves the default partition ip address.",
			requestingCtx:    defaultClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestWithNoPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.default.ap.consul resolves the default partition ip address.",
			requestingCtx:    defaultClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify the unexported static-server.service.secondary.ap.consul from the default partition. With ACLs turned on, this should not resolve. Otherwise, it will resolve.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: shouldResolveUnexportedCrossPartitionDNSRecord,
		},
		{
			name:             "verify static-server.service.secondary.ap.consul from the secondary partition.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.consul from the secondary partition should return the ip in the secondary.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestWithNoPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.default.ap.consul from the secondary partition. With ACLs turned on, this should not resolve. Otherwise, it will resolve.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: shouldResolveUnexportedCrossPartitionDNSRecord,
		},
		{
			name:             "verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
		{
			name:             "verify static-server.service.default.ap.consul from the secondary partition once the service is exported.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				k8s.KubectlApplyK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				})
			},
		},
		{
			name:             "after rollout restart of dns-proxy in default partition - verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				restartDNSProxy(t, releaseName, defaultClusterContext)
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
		{
			name:             "after rollout restart of dns-proxy in secondary partition - verify static-server.service.default.ap.consul from the secondary partition once the service is exported.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				restartDNSProxy(t, releaseName, secondaryClusterContext)
				k8s.KubectlApplyK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				})
			},
		},
		{
			name:             "flip default cluster to use DNS service instead - verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				defaultConsulCluster.Upgrade(t, map[string]string{"dns.proxy.enabled": "false"})
				updateCoreDNSWithConsulDomain_Privileged(t, defaultClusterContext, releaseName, false)
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
		{
			name:             "flip default cluster back to using DNS Proxy - verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				defaultConsulCluster.Upgrade(t, map[string]string{"dns.proxy.enabled": "true"})
				updateCoreDNSWithConsulDomain_Privileged(t, defaultClusterContext, releaseName, true)
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
	}

	return verifications
}
