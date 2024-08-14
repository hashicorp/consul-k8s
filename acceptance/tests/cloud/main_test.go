// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloud

import (
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)
	os.Exit(suite.Run())
}
