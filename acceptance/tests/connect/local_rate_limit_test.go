// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"testing"
	"time"

	terratestK8s "github.com/gruntwork-io/terratest/modules/k8s"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// TestConnectInject_LocalRateLimiting tests that local rate limiting works as expected between services.
func TestConnectInject_LocalRateLimiting(t *testing.T) {
	t.Skipf("TODO(flaky-1.17): NET-XXXX")
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	releaseName := helpers.RandomName()
	connHelper := connhelper.ConnectHelper{
		ClusterKind:     consul.Helm,
		Secure:          false,
		ReleaseName:     releaseName,
		Ctx:             ctx,
		UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
		Cfg:             cfg,
	}

	connHelper.Setup(t)
	connHelper.Install(t)
	connHelper.DeployClientAndServer(t)
	connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})

	// Map the static-server URL and path to the rate limits defined in the service defaults at:
	// ../fixtures/cases/local-rate-limiting/service-defaults-static-server.yaml
	rateLimitMap := map[string]int{
		"http://localhost:1234":             2,
		"http://localhost:1234/exact":       3,
		"http://localhost:1234/prefix-test": 4,
		"http://localhost:1234/regex":       5,
	}

	opts := newRateLimitOptions(t, ctx)

	// Ensure that all requests from static-client to static-server succeed (no rate limiting set).
	for addr, rps := range rateLimitMap {
		opts.rps = rps
		assertRateLimits(t, opts, addr)
	}

	// Apply local rate limiting to the static-server
	writeCrd(t, connHelper, "../fixtures/cases/local-rate-limiting")

	// Ensure that going over the limit causes the static-server to apply rate limiting and
	// reply with 429 Too Many Requests
	opts.enforced = true
	for addr, reqPerSec := range rateLimitMap {
		opts.rps = reqPerSec
		assertRateLimits(t, opts, addr)
	}
}

func assertRateLimits(t *testing.T, opts *assertRateLimitOptions, curlArgs ...string) {
	t.Helper()

	args := []string{"exec", opts.resourceType + opts.sourceApp, "-c", opts.sourceApp, "--", "curl", opts.curlOpts}
	args = append(args, curlArgs...)

	// This check is time sensitive due to the nature of rate limiting.
	// Run the entire assertion in a retry block and on each pass:
	// 1. Send the exact number of requests that are allowed per the rate limiting configuration
	//    and check that all the requests succeed.
	// 2. Send an extra request that should exceed the configured rate limit and check that this request fails.
	// 3. Make sure that all requests happened within the rate limit enforcement window of one second.
	retry.RunWith(opts.retryTimer, t, func(r *retry.R) {
		// Make up to the allowed numbers of calls in a second
		t0 := time.Now()
		for i := 0; i < opts.rps; i++ {
			output, err := k8s.RunKubectlAndGetOutputE(t, opts.k8sOpts, args...)
			require.NoError(r, err)
			require.Contains(r, output, opts.successOutput)
		}

		// Exceed the configured rate limit.
		output, err := k8s.RunKubectlAndGetOutputE(t, opts.k8sOpts, args...)
		if opts.enforced {
			require.Error(r, err)
			require.Contains(r, output, opts.rateLimitOutput, "request was not rate limited")
		} else {
			require.NoError(r, err)
			require.Contains(r, output, opts.successOutput, "request was not successful")
		}
		require.True(r, time.Since(t0) < time.Second, "failed to make all requests within one second window")
	})
}

type assertRateLimitOptions struct {
	resourceType    string
	successOutput   string
	rateLimitOutput string
	k8sOpts         *terratestK8s.KubectlOptions
	sourceApp       string
	rps             int
	enforced        bool
	retryTimer      *retry.Timer
	curlOpts        string
}

func newRateLimitOptions(t *testing.T, ctx environment.TestContext) *assertRateLimitOptions {
	return &assertRateLimitOptions{
		resourceType:    "deploy/",
		successOutput:   "hello world",
		rateLimitOutput: "curl: (22) The requested URL returned error: 429",
		k8sOpts:         ctx.KubectlOptions(t),
		sourceApp:       connhelper.StaticClientName,
		retryTimer:      &retry.Timer{Timeout: 120 * time.Second, Wait: 2 * time.Second},
		curlOpts:        "-vvvsSf",
	}
}
