// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"os"
	"testing"
	"time"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
	"github.com/hashicorp/consul/sdk/testutil/retry"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)
	os.Exit(suite.Run())
}

func retryCheck(t *testing.T, count int, wait time.Duration, fn func(r *retry.R)) {
	t.Helper()

	counter := &retry.Counter{Count: count, Wait: wait}
	retry.RunWith(counter, t, fn)
}
