// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
)

// ResourceTester is a helper for making assertions about resources.
type ResourceTester struct {
	// resourceClient is the client to use for resource operations.
	resourceClient pbresource.ResourceServiceClient
	// timeout is the timeout to use for the retryer.
	timeout time.Duration
	// wait is the total wait time to use for the retryer.
	wait time.Duration
	// token is the token to use for requests when ACLs are enabled.
	token string
}

func NewResourceTester(resourceClient pbresource.ResourceServiceClient) *ResourceTester {
	return &ResourceTester{
		resourceClient: resourceClient,
		timeout:        7 * time.Second,
		wait:           25 * time.Millisecond,
	}
}

func (r *ResourceTester) retry(t testutil.TestingTB, fn func(r *retry.R)) {
	t.Helper()
	retryer := &retry.Timer{Timeout: r.timeout, Wait: r.wait}
	retry.RunWith(retryer, t, fn)
}

func (rh *ResourceTester) Context(t testutil.TestingTB) context.Context {
	ctx := testutil.TestContext(t)

	if rh.token != "" {
		md := metadata.New(map[string]string{
			"x-consul-token": rh.token,
		})
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	return ctx
}

func (rh *ResourceTester) RequireResourceExists(t testutil.TestingTB, id *pbresource.ID) *pbresource.Resource {
	t.Helper()

	rsp, err := rh.resourceClient.Read(rh.Context(t), &pbresource.ReadRequest{Id: id})
	require.NoError(t, err, "error reading %s with type %v", id.Name, id.Type)
	require.NotNil(t, rsp)
	return rsp.Resource
}

func (rh *ResourceTester) RequireResourceNotFound(t testutil.TestingTB, id *pbresource.ID) {
	t.Helper()

	rsp, err := rh.resourceClient.Read(rh.Context(t), &pbresource.ReadRequest{Id: id})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Nil(t, rsp)
}

func (rh *ResourceTester) WaitForResourceExists(t testutil.TestingTB, id *pbresource.ID) *pbresource.Resource {
	t.Helper()

	var res *pbresource.Resource
	rh.retry(t, func(r *retry.R) {
		res = rh.RequireResourceExists(r, id)
	})

	return res
}

func (rh *ResourceTester) WaitForResourceNotFound(t testutil.TestingTB, id *pbresource.ID) {
	t.Helper()

	rh.retry(t, func(r *retry.R) {
		rh.RequireResourceNotFound(r, id)
	})
}
