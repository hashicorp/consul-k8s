package serveraclinit

import (
	"fmt"
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

func TestAnonymousTokenRules(t *testing.T) {
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

			rules, err := cmd.anonymousTokenRules()

			require.NoError(err)
			require.Equal(tt.Expected, rules)
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

func TestIngressGatewayRules(t *testing.T) {
	cases := []struct {
		Name             string
		GatewayName      string
		GatewayNamespace string
		EnableNamespaces bool
		Expected         string
	}{
		{
			"Namespaces are disabled",
			"ingress-gateway",
			"",
			false,
			`
  service "ingress-gateway" {
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
			"gateway",
			"default",
			true,
			`
namespace "default" {
  service "gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "read"
  }
}`,
		},
		{
			"Namespaces are enabled, non-default namespace",
			"gateway",
			"non-default",
			true,
			`
namespace "non-default" {
  service "gateway" {
     policy = "write"
  }
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

			ingressGatewayRules, err := cmd.ingressGatewayRules(tt.GatewayName, tt.GatewayNamespace)

			require.NoError(err)
			require.Equal(tt.Expected, ingressGatewayRules)
		})
	}
}

func TestTerminatingGatewayRules(t *testing.T) {
	cases := []struct {
		Name             string
		GatewayName      string
		GatewayNamespace string
		EnableNamespaces bool
		Expected         string
	}{
		{
			"Namespaces are disabled",
			"terminating-gateway",
			"",
			false,
			`
  service "terminating-gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }`,
		},
		{
			"Namespaces are enabled",
			"gateway",
			"default",
			true,
			`
namespace "default" {
  service "gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
}`,
		},
		{
			"Namespaces are enabled, non-default namespace",
			"gateway",
			"non-default",
			true,
			`
namespace "non-default" {
  service "gateway" {
     policy = "write"
  }
  node_prefix "" {
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

			terminatingGatewayRules, err := cmd.terminatingGatewayRules(tt.GatewayName, tt.GatewayNamespace)

			require.NoError(err)
			require.Equal(tt.Expected, terminatingGatewayRules)
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
		SyncConsulNodeName             string
		Expected                       string
	}{
		{
			"Namespaces are disabled",
			false,
			"sync-namespace",
			true,
			"prefix-",
			"k8s-sync",
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
			"Namespaces are disabled, non-default node name",
			false,
			"sync-namespace",
			true,
			"prefix-",
			"new-node-name",
			`node "new-node-name" {
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
			"k8s-sync",
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
			"Namespaces are enabled, mirroring disabled, non-default node name",
			true,
			"sync-namespace",
			false,
			"prefix-",
			"new-node-name",
			`node "new-node-name" {
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
			"k8s-sync",
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
			"Namespaces are enabled, mirroring enabled, prefix empty, non-default node name",
			true,
			"sync-namespace",
			true,
			"",
			"new-node-name",
			`node "new-node-name" {
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
			"k8s-sync",
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
		{
			"Namespaces are enabled, mirroring enabled, prefix defined, non-default node name",
			true,
			"sync-namespace",
			true,
			"prefix-",
			"new-node-name",
			`node "new-node-name" {
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
				flagSyncConsulNodeName:             tt.SyncConsulNodeName,
			}

			syncRules, err := cmd.syncRules()

			require.NoError(err)
			require.Equal(tt.Expected, syncRules)
		})
	}
}

// Test the inject rules with namespaces enabled or disabled.
func TestInjectRules(t *testing.T) {
	cases := []struct {
		EnableNamespaces bool
		Expected         string
	}{
		{
			EnableNamespaces: false,
			Expected: `
node_prefix "" {
  policy = "write"
}
  service_prefix "" {
    policy = "write"
  }`,
		},
		{
			EnableNamespaces: true,
			Expected: `
operator = "write"
node_prefix "" {
  policy = "write"
}
namespace_prefix "" {
  service_prefix "" {
    policy = "write"
  }
}`,
		},
	}

	for _, tt := range cases {
		caseName := fmt.Sprintf("ns=%t", tt.EnableNamespaces)
		t.Run(caseName, func(t *testing.T) {
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
			`operator = "write"
agent_prefix "" {
  policy = "read"
}
node_prefix "" {
  policy = "write"
}
  acl = "write"
  service_prefix "" {
    policy = "read"
    intentions = "read"
  }`,
		},
		{
			"Namespaces are enabled",
			true,
			`operator = "write"
agent_prefix "" {
  policy = "read"
}
node_prefix "" {
  policy = "write"
}
namespace_prefix "" {
  acl = "write"
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

func TestControllerRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnableNamespaces bool
		DestConsulNS     string
		Mirroring        bool
		MirroringPrefix  string
		Expected         string
	}{
		{
			Name:             "namespaces=disabled",
			EnableNamespaces: false,
			Expected: `operator = "write"
  service_prefix "" {
    policy = "write"
    intentions = "write"
  }`,
		},
		{
			Name:             "namespaces=enabled, consulDestNS=consul",
			EnableNamespaces: true,
			DestConsulNS:     "consul",
			Expected: `operator = "write"
namespace "consul" {
  service_prefix "" {
    policy = "write"
    intentions = "write"
  }
}`,
		},
		{
			Name:             "namespaces=enabled, mirroring=true",
			EnableNamespaces: true,
			Mirroring:        true,
			Expected: `operator = "write"
namespace_prefix "" {
  service_prefix "" {
    policy = "write"
    intentions = "write"
  }
}`,
		},
		{
			Name:             "namespaces=enabled, mirroring=true, mirroringPrefix=prefix-",
			EnableNamespaces: true,
			Mirroring:        true,
			MirroringPrefix:  "prefix-",
			Expected: `operator = "write"
namespace_prefix "prefix-" {
  service_prefix "" {
    policy = "write"
    intentions = "write"
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			cmd := Command{
				flagEnableNamespaces:                 tt.EnableNamespaces,
				flagConsulInjectDestinationNamespace: tt.DestConsulNS,
				flagEnableInjectK8SNSMirroring:       tt.Mirroring,
				flagInjectK8SNSMirroringPrefix:       tt.MirroringPrefix,
			}

			rules, err := cmd.controllerRules()

			require.NoError(err)
			require.Equal(tt.Expected, rules)
		})
	}
}
