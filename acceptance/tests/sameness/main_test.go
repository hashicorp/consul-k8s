// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package sameness

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

const expectedNumberOfClusters = 4

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	if samenessTestConfigSupported(suite.Config()) {
		os.Exit(suite.Run())
	}

	fmt.Printf("Skipping sameness tests: requires -enable-multi-cluster, at least %d clusters, "+
		"and -use-kind, -enable-openshift or -use-openshift.\n", expectedNumberOfClusters)
}

func samenessTestConfigSupported(cfg *config.TestConfig) bool {
	return cfg.EnableMultiCluster && cfg.IsExpectedClusterCount(expectedNumberOfClusters) &&
		(cfg.UseKind || cfg.EnableOpenshift || cfg.UseOpenshift)
}
