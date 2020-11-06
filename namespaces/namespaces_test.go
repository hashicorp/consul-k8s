// +build enterprise

package namespaces

import (
	"fmt"
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/serf/testutil/retry"
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
			masterToken := "master"

			consul, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				cfg.ACL.Enabled = c.ACLsEnabled
				cfg.ACL.DefaultPolicy = "deny"
				cfg.ACL.Tokens.Master = masterToken
			})
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
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
			created, err := EnsureExists(consulClient, ns, crossNSPolicy)
			req.NoError(err)
			require.False(t, created)

			// Ensure it still exists.
			consulNS, _, err := consulClient.Namespaces().Read(ns, nil)
			req.NoError(err)
			// We can test that it wasn't updated by checking that the cross
			// namespace ACL policy wasn't added (if running with acls).
			if c.ACLsEnabled {
				require.Nil(t, consulNS.ACLs)
			}
		})
	}
}

// Test that if the namespace already exists the function succeeds.
func TestEnsureExists_WildcardNamespace_AlreadyExists(tt *testing.T) {
	created, err := EnsureExists(nil, WildcardNamespace, "")
	require.NoError(tt, err)
	require.False(tt, created)
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
			masterToken := "master"

			consul, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				cfg.ACL.Enabled = c.ACLsEnabled
				cfg.ACL.DefaultPolicy = "deny"
				cfg.ACL.Tokens.Master = masterToken
			})
			req.NoError(err)
			defer consul.Stop()

			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			})
			req.NoError(err)

			// Need to loop to ensure Consul is up.
			timer := &retry.Timer{Timeout: 5 * time.Second, Wait: 500 * time.Millisecond}
			retry.RunWith(timer, tt, func(r *retry.R) {
				leader, err := consulClient.Status().Leader()
				require.NoError(r, err)
				require.NotEmpty(r, leader)
			})

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

			created, err := EnsureExists(consulClient, ns, crossNSPolicy)
			req.NoError(err)
			require.True(t, created)

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

func TestConsulNamespace(t *testing.T) {
	cases := map[string]struct {
		kubeNS                 string
		enableConsulNamespaces bool
		consulDestNS           string
		enableMirroring        bool
		mirroringPrefix        string
		expNS                  string
	}{
		"namespaces disabled": {
			enableConsulNamespaces: false,
			expNS:                  "",
		},
		"mirroring": {
			enableConsulNamespaces: true,
			enableMirroring:        true,
			kubeNS:                 "kube",
			expNS:                  "kube",
		},
		"mirroring with prefix": {
			enableConsulNamespaces: true,
			enableMirroring:        true,
			mirroringPrefix:        "prefix-",
			kubeNS:                 "kube",
			expNS:                  "prefix-kube",
		},
		"destination consul ns": {
			enableConsulNamespaces: true,
			consulDestNS:           "dest",
			kubeNS:                 "kube",
			expNS:                  "dest",
		},
		"mirroring takes precedence": {
			enableConsulNamespaces: true,
			consulDestNS:           "dest",
			enableMirroring:        true,
			kubeNS:                 "kube",
			expNS:                  "kube",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := ConsulNamespace(c.kubeNS, c.enableConsulNamespaces, c.consulDestNS, c.enableMirroring, c.mirroringPrefix)
			require.Equal(t, c.expNS, act)
		})
	}
}
