package serveraclinit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnableNamespaces bool
		Expected         string
	}{
		{
			"Namespaces are disabled",
			false,
			`node_prefix "" {
    policy = "write"
  }
  service_prefix "" {
    policy = "read"
  }`,
		},
		{
			"Namespaces are enabled",
			true,
			`node_prefix "" {
    policy = "write"
  }
namespace_prefix "" {
  service_prefix "" {
    policy = "read"
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			cmd := Command{
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			agentRules, err := cmd.agentRules()

			require.NoError(err)
			require.Equal(tt.Expected, agentRules)
		})
	}
}

func TestDNSRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnableNamespaces bool
		Expected         string
	}{
		{
			"Namespaces are disabled",
			false,
			`
  node_prefix "" {
     policy = "read"
  }
  service_prefix "" {
     policy = "read"
  }`,
		},
		{
			"Namespaces are enabled",
			true,
			`
namespace_prefix "" {
  node_prefix "" {
     policy = "read"
  }
  service_prefix "" {
     policy = "read"
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			cmd := Command{
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			dnsRules, err := cmd.dnsRules()

			require.NoError(err)
			require.Equal(tt.Expected, dnsRules)
		})
	}
}

func TestMeshGatewayRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnableNamespaces bool
		Expected         string
	}{
		{
			"Namespaces are disabled",
			false,
			`agent_prefix "" {
  	policy = "read"
  }
  service "mesh-gateway" {
     policy = "write"
  }
  node_prefix "" {
  	policy = "read"
  }
  service_prefix "" {
     policy = "read"
  }`,
		},
		{
			"Namespaces are enabled",
			true,
			`agent_prefix "" {
  	policy = "read"
  }
namespace "default" {
  service "mesh-gateway" {
     policy = "write"
  }
}
namespace_prefix "" {
  node_prefix "" {
  	policy = "read"
  }
  service_prefix "" {
     policy = "read"
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			cmd := Command{
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			meshGatewayRules, err := cmd.meshGatewayRules()

			require.NoError(err)
			require.Equal(tt.Expected, meshGatewayRules)
		})
	}
}

func TestSyncRules(t *testing.T) {
	cases := []struct {
		Name                           string
		EnableNamespaces               bool
		ConsulSyncDestinationNamespace string
		EnableSyncK8SNSMirroring       bool
		SyncK8SNSMirroringPrefix       string
		Expected                       string
	}{
		{
			"Namespaces are disabled",
			false,
			"sync-namespace",
			true,
			"prefix-",
			`node "k8s-sync" {
    policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "write"
  }`,
		},
		{
			"Namespaces are enabled, mirroring disabled",
			true,
			"sync-namespace",
			false,
			"prefix-",
			`node "k8s-sync" {
    policy = "write"
  }
operator = "write"
namespace "sync-namespace" {
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "write"
  }
}`,
		},
		{
			"Namespaces are enabled, mirroring enabled, prefix empty",
			true,
			"sync-namespace",
			true,
			"",
			`node "k8s-sync" {
    policy = "write"
  }
operator = "write"
namespace_prefix "" {
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "write"
  }
}`,
		},
		{
			"Namespaces are enabled, mirroring enabled, prefix defined",
			true,
			"sync-namespace",
			true,
			"prefix-",
			`node "k8s-sync" {
    policy = "write"
  }
operator = "write"
namespace_prefix "prefix-" {
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "write"
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			cmd := Command{
				flagEnableNamespaces:               tt.EnableNamespaces,
				flagConsulSyncDestinationNamespace: tt.ConsulSyncDestinationNamespace,
				flagEnableSyncK8SNSMirroring:       tt.EnableSyncK8SNSMirroring,
				flagSyncK8SNSMirroringPrefix:       tt.SyncK8SNSMirroringPrefix,
			}

			syncRules, err := cmd.syncRules()

			require.NoError(err)
			require.Equal(tt.Expected, syncRules)
		})
	}
}

func TestInjectRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnableNamespaces bool
		Expected         string
	}{
		{
			"Namespaces are disabled",
			false,
			"",
		},
		{
			"Namespaces are enabled",
			true,
			`
operator = "write"`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			cmd := Command{
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			injectorRules, err := cmd.injectRules()

			require.NoError(err)
			require.Equal(tt.Expected, injectorRules)
		})
	}
}

func TestReplicationTokenRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnableNamespaces bool
		Expected         string
	}{
		{
			"Namespaces are disabled",
			false,
			`acl = "write"
operator = "write"
  node_prefix "" {
    policy = "write"
  }
  service_prefix "" {
    policy = "read"
    intentions = "read"
  }`,
		},
		{
			"Namespaces are enabled",
			true,
			`acl = "write"
operator = "write"
namespace_prefix "" {
  node_prefix "" {
    policy = "write"
  }
  service_prefix "" {
    policy = "read"
    intentions = "read"
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			cmd := Command{
				flagEnableNamespaces: tt.EnableNamespaces,
			}
			replicationTokenRules, err := cmd.aclReplicationRules()
			require.NoError(err)
			require.Equal(tt.Expected, replicationTokenRules)
		})
	}
}
