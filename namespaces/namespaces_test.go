// +build enterprise

package namespaces

import (
	"fmt"
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// Test that if the namespace already exists the function succeeds.
func TestEnsureExists_AlreadyExists(tt *testing.T) {
	for _, c := range []struct {
		ACLsEnabled bool
	}{
		{
			ACLsEnabled: true,
		},
		{
			ACLsEnabled: false,
		},
	} {
		tt.Run(fmt.Sprintf("acls: %t", c.ACLsEnabled), func(t *testing.T) {
			req := require.New(t)
			ns := "ns"

			consul, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				cfg.ACL.Enabled = c.ACLsEnabled
				cfg.ACL.DefaultPolicy = "deny"
			})
			req.NoError(err)
			defer consul.Stop()

			var bootstrapToken string
			if c.ACLsEnabled {
				// Set up a client for bootstrapping
				bootClient, err := capi.NewClient(&capi.Config{
					Address: consul.HTTPAddr,
				})
				require.NoError(t, err)

				// Bootstrap the server and get the bootstrap token
				var bootstrapResp *capi.ACLToken
				timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
				retry.RunWith(timer, t, func(r *retry.R) {
					bootstrapResp, _, err = bootClient.ACL().Bootstrap()
					require.NoError(r, err)
				})
				bootstrapToken = bootstrapResp.SecretID
				require.NotEmpty(t, bootstrapToken)
			}

			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
				Token:   bootstrapToken,
			})
			req.NoError(err)

			// Pre-create the namespace.
			_, _, err = consulClient.Namespaces().Create(&capi.Namespace{
				Name: ns,
			}, nil)
			req.NoError(err)

			var crossNSPolicy string
			if c.ACLsEnabled {
				crossNSPolicy = "cross-ns-policy"
			}
			err = EnsureExists(consulClient, ns, crossNSPolicy)
			req.NoError(err)

			// Ensure it still exists.
			_, _, err = consulClient.Namespaces().Read(ns, nil)
			req.NoError(err)
		})
	}
}

// Test that it creates the namespace if it doesn't exist.
func TestEnsureExists_CreatesNS(tt *testing.T) {
	for _, c := range []struct {
		ACLsEnabled bool
	}{
		{
			ACLsEnabled: true,
		},
		{
			ACLsEnabled: false,
		},
	} {
		tt.Run(fmt.Sprintf("acls: %t", c.ACLsEnabled), func(t *testing.T) {
			req := require.New(t)
			ns := "ns"

			consul, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				cfg.ACL.Enabled = c.ACLsEnabled
				cfg.ACL.DefaultPolicy = "deny"
			})
			req.NoError(err)
			defer consul.Stop()

			var bootstrapToken string
			if c.ACLsEnabled {
				// Set up a client for bootstrapping
				bootClient, err := capi.NewClient(&capi.Config{
					Address: consul.HTTPAddr,
				})
				require.NoError(t, err)

				// Bootstrap the server and get the bootstrap token
				var bootstrapResp *capi.ACLToken
				timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
				retry.RunWith(timer, t, func(r *retry.R) {
					bootstrapResp, _, err = bootClient.ACL().Bootstrap()
					require.NoError(r, err)
				})
				bootstrapToken = bootstrapResp.SecretID
				require.NotEmpty(t, bootstrapToken)
			}

			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
				Token:   bootstrapToken,
			})
			req.NoError(err)

			var crossNSPolicy string
			if c.ACLsEnabled {
				crossNSPolicy = "cross-ns-policy"
			}

			if c.ACLsEnabled {
				// Must pre-create the cross-ns policy.
				_, _, err = consulClient.ACL().PolicyCreate(&capi.ACLPolicy{
					Name: crossNSPolicy,
				}, nil)
				req.NoError(err)
			}

			err = EnsureExists(consulClient, ns, crossNSPolicy)
			req.NoError(err)

			// Ensure it was created.
			cNS, _, err := consulClient.Namespaces().Read(ns, nil)
			req.NoError(err)
			req.Equal("Auto-generated by consul-k8s", cNS.Description)

			if c.ACLsEnabled {
				req.Len(cNS.ACLs.PolicyDefaults, 1)
				req.Equal(cNS.ACLs.PolicyDefaults[0].Name, crossNSPolicy)
			} else {
				req.Len(cNS.ACLs.PolicyDefaults, 0)
			}
		})
	}
}
