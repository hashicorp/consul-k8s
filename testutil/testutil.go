package testutil

import (
	"testing"

	capi "github.com/hashicorp/consul/api"
	ctestutil "github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
)

func NewConsulTestServer(t *testing.T, cb ctestutil.ServerConfigCallback) *capi.Client {
	server, err := ctestutil.NewTestServerConfigT(t, cb)
	require.NoError(t, err)
	t.Cleanup(func() {
		server.Stop()
	})
	server.WaitForLeader(t)

	client, err := capi.NewClient(&capi.Config{
		Address: server.HTTPAddr,
	})
	require.NoError(t, err)
	return client
}
