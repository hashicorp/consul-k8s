package serveraclinit

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnablePartitions bool
		PartitionName    string
		EnableNamespaces bool
		Expected         string
	}{
		{
			Name: "Namespaces and Partitions are disabled",
			Expected: `
  node_prefix "" {
    policy = "write"
  }
    service_prefix "" {
      policy = "read"
    }`,
		},
		{
			Name:             "Namespaces are enabled, Partitions are disabled",
			EnableNamespaces: true,
			Expected: `
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    service_prefix "" {
      policy = "read"
    }
  }`,
		},
		{
			Name:             "Namespaces and Partitions are enabled",
			EnablePartitions: true,
			PartitionName:    "part-1",
			EnableNamespaces: true,
			Expected: `
partition "part-1" {
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    service_prefix "" {
      policy = "read"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				flagEnablePartitions: tt.EnablePartitions,
				flagPartitionName:    tt.PartitionName,
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			agentRules, err := cmd.agentRules()

			require.NoError(t, err)
			require.Equal(t, tt.Expected, agentRules)
		})
	}
}

func TestAnonymousTokenRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnablePartitions bool
		PartitionName    string
		EnableNamespaces bool
		Expected         string
	}{
		{
			Name: "Namespaces and Partitions are disabled",
			Expected: `
    node_prefix "" {
       policy = "read"
    }
    service_prefix "" {
       policy = "read"
    }`,
		},
		{
			Name:             "Namespaces are enabled, Partitions are disabled",
			EnableNamespaces: true,
			Expected: `
  namespace_prefix "" {
    node_prefix "" {
       policy = "read"
    }
    service_prefix "" {
       policy = "read"
    }
  }`,
		},
		{
			Name:             "Namespaces and Partitions are enabled",
			EnablePartitions: true,
			PartitionName:    "part-2",
			EnableNamespaces: true,
			Expected: `
partition_prefix "" {
  namespace_prefix "" {
    node_prefix "" {
       policy = "read"
    }
    service_prefix "" {
       policy = "read"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				flagEnablePartitions: tt.EnablePartitions,
				flagPartitionName:    tt.PartitionName,
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			rules, err := cmd.anonymousTokenRules()

			require.NoError(t, err)
			require.Equal(t, tt.Expected, rules)
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
			Name: "Namespaces are disabled",
			Expected: `agent_prefix "" {
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
			Name:             "Namespaces are enabled",
			EnableNamespaces: true,
			Expected: `agent_prefix "" {
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
			cmd := Command{
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			meshGatewayRules, err := cmd.meshGatewayRules()

			require.NoError(t, err)
			require.Equal(t, tt.Expected, meshGatewayRules)
		})
	}
}

func TestIngressGatewayRules(t *testing.T) {
	cases := []struct {
		Name             string
		GatewayName      string
		GatewayNamespace string
		EnablePartitions bool
		PartitionName    string
		EnableNamespaces bool
		Expected         string
	}{
		{
			Name:        "Namespaces and Partitions are disabled",
			GatewayName: "ingress-gateway",
			Expected: `
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
			Name:             "Namespaces are enabled, Partitions are disabled",
			GatewayName:      "gateway",
			GatewayNamespace: "default",
			EnableNamespaces: true,
			Expected: `
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
			Name:             "Namespaces are enabled, non-default namespace, Partitions are disabled",
			GatewayName:      "gateway",
			GatewayNamespace: "non-default",
			EnableNamespaces: true,
			Expected: `
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
		{
			Name:             "Namespaces and Partitions are enabled",
			GatewayName:      "gateway",
			GatewayNamespace: "default",
			EnableNamespaces: true,
			EnablePartitions: true,
			PartitionName:    "part-1",
			Expected: `
partition "part-1" {
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
  }
}`,
		},
		{
			Name:             "Namespaces and Partitions are enabled, non-default namespace",
			GatewayName:      "gateway",
			GatewayNamespace: "non-default",
			EnableNamespaces: true,
			EnablePartitions: true,
			PartitionName:    "default",
			Expected: `
partition "default" {
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
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				flagEnablePartitions: tt.EnablePartitions,
				flagPartitionName:    tt.PartitionName,
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			ingressGatewayRules, err := cmd.ingressGatewayRules(tt.GatewayName, tt.GatewayNamespace)

			require.NoError(t, err)
			require.Equal(t, tt.Expected, ingressGatewayRules)
		})
	}
}

func TestTerminatingGatewayRules(t *testing.T) {
	cases := []struct {
		Name             string
		GatewayName      string
		GatewayNamespace string
		EnableNamespaces bool
		EnablePartitions bool
		PartitionName    string
		Expected         string
	}{
		{
			Name:        "Namespaces and Partitions are disabled",
			GatewayName: "terminating-gateway",
			Expected: `
    service "terminating-gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }`,
		},
		{
			Name:             "Namespaces are enabled, Partitions are disabled",
			GatewayName:      "gateway",
			GatewayNamespace: "default",
			EnableNamespaces: true,
			Expected: `
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
			Name:             "Namespaces are enabled, non-default namespace, Partitions are disabled",
			GatewayName:      "gateway",
			GatewayNamespace: "non-default",
			EnableNamespaces: true,
			Expected: `
  namespace "non-default" {
    service "gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }`,
		},
		{
			Name:             "Namespaces and Partitions are enabled",
			GatewayName:      "gateway",
			GatewayNamespace: "default",
			EnableNamespaces: true,
			EnablePartitions: true,
			PartitionName:    "part-1",
			Expected: `
partition "part-1" {
  namespace "default" {
    service "gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`,
		},
		{
			Name:             "Namespaces and Partitions are enabled, non-default namespace",
			GatewayName:      "gateway",
			GatewayNamespace: "non-default",
			EnableNamespaces: true,
			EnablePartitions: true,
			PartitionName:    "default",
			Expected: `
partition "default" {
  namespace "non-default" {
    service "gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				flagEnablePartitions: tt.EnablePartitions,
				flagPartitionName:    tt.PartitionName,
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			terminatingGatewayRules, err := cmd.terminatingGatewayRules(tt.GatewayName, tt.GatewayNamespace)

			require.NoError(t, err)
			require.Equal(t, tt.Expected, terminatingGatewayRules)
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
			Name:                           "Namespaces are disabled",
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "k8s-sync",
			Expected: `node "k8s-sync" {
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
			Name:                           "Namespaces are disabled, non-default node name",
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "new-node-name",
			Expected: `node "new-node-name" {
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
			Name:                           "Namespaces are enabled, mirroring disabled",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "k8s-sync",
			Expected: `node "k8s-sync" {
    policy = "write"
  }
operator = "write"
acl = "write"
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
			Name:                           "Namespaces are enabled, mirroring disabled, non-default node name",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "new-node-name",
			Expected: `node "new-node-name" {
    policy = "write"
  }
operator = "write"
acl = "write"
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
			Name:                           "Namespaces are enabled, mirroring enabled, prefix empty",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncConsulNodeName:             "k8s-sync",
			Expected: `node "k8s-sync" {
    policy = "write"
  }
operator = "write"
acl = "write"
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
			Name:                           "Namespaces are enabled, mirroring enabled, prefix empty, non-default node name",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncConsulNodeName:             "new-node-name",
			Expected: `node "new-node-name" {
    policy = "write"
  }
operator = "write"
acl = "write"
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
			Name:                           "Namespaces are enabled, mirroring enabled, prefix defined",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "k8s-sync",
			Expected: `node "k8s-sync" {
    policy = "write"
  }
operator = "write"
acl = "write"
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
			Name:                           "Namespaces are enabled, mirroring enabled, prefix defined, non-default node name",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "new-node-name",
			Expected: `node "new-node-name" {
    policy = "write"
  }
operator = "write"
acl = "write"
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
			cmd := Command{
				flagEnableNamespaces:               tt.EnableNamespaces,
				flagConsulSyncDestinationNamespace: tt.ConsulSyncDestinationNamespace,
				flagEnableSyncK8SNSMirroring:       tt.EnableSyncK8SNSMirroring,
				flagSyncK8SNSMirroringPrefix:       tt.SyncK8SNSMirroringPrefix,
				flagSyncConsulNodeName:             tt.SyncConsulNodeName,
			}

			syncRules, err := cmd.syncRules()

			require.NoError(t, err)
			require.Equal(t, tt.Expected, syncRules)
		})
	}
}

// Test the inject rules with namespaces enabled or disabled.
func TestInjectRules(t *testing.T) {
	cases := []struct {
		EnableNamespaces bool
		EnablePartitions bool
		PartitionName    string
		Expected         string
	}{
		{
			EnableNamespaces: false,
			EnablePartitions: false,
			Expected: `
  node_prefix "" {
    policy = "write"
  }
    acl = "write"
    service_prefix "" {
      policy = "write"
    }`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: false,
			Expected: `
  operator = "write"
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    acl = "write"
    service_prefix "" {
      policy = "write"
    }
  }`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: true,
			PartitionName:    "part-1",
			Expected: `
partition "part-1" {
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    policy = "write"
    acl = "write"
    service_prefix "" {
      policy = "write"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		caseName := fmt.Sprintf("ns=%t, partition=%t", tt.EnableNamespaces, tt.EnablePartitions)
		t.Run(caseName, func(t *testing.T) {

			cmd := Command{
				flagEnablePartitions: tt.EnablePartitions,
				flagPartitionName:    tt.PartitionName,
				flagEnableNamespaces: tt.EnableNamespaces,
			}

			injectorRules, err := cmd.injectRules()

			require.NoError(t, err)
			require.Equal(t, tt.Expected, injectorRules)
		})
	}
}

func TestReplicationTokenRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnableNamespaces bool
		EnablePartitions bool
		PartitionName    string
		Expected         string
	}{
		{
			Name: "Namespaces and Partitions are disabled",
			Expected: `
  operator = "write"
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
			Name:             "Namespaces are enabled, Partitions are disabled",
			EnableNamespaces: true,
			Expected: `
  operator = "write"
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
		{
			Name:             "Namespaces and Partitions are enabled, default partition",
			EnableNamespaces: true,
			EnablePartitions: true,
			PartitionName:    "default",
			Expected: `
partition "default" {
  operator = "write"
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
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				flagEnablePartitions: tt.EnablePartitions,
				flagPartitionName:    tt.PartitionName,
				flagEnableNamespaces: tt.EnableNamespaces,
			}
			replicationTokenRules, err := cmd.aclReplicationRules()
			require.NoError(t, err)
			require.Equal(t, tt.Expected, replicationTokenRules)
		})
	}
}

func TestControllerRules(t *testing.T) {
	cases := []struct {
		Name             string
		EnablePartitions bool
		PartitionName    string
		EnableNamespaces bool
		DestConsulNS     string
		Mirroring        bool
		MirroringPrefix  string
		Expected         string
	}{
		{
			Name: "namespaces=disabled, partitions=disabled",
			Expected: `
  operator = "write"
  acl = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }`,
		},
		{
			Name:             "namespaces=enabled, consulDestNS=consul, partitions=disabled",
			EnableNamespaces: true,
			DestConsulNS:     "consul",
			Expected: `
  operator = "write"
  acl = "write"
  namespace "consul" {
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }`,
		},
		{
			Name:             "namespaces=enabled, mirroring=true, partitions=disabled",
			EnableNamespaces: true,
			Mirroring:        true,
			Expected: `
  operator = "write"
  acl = "write"
  namespace_prefix "" {
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }`,
		},
		{
			Name:             "namespaces=enabled, mirroring=true, mirroringPrefix=prefix-, partitions=disabled",
			EnableNamespaces: true,
			Mirroring:        true,
			MirroringPrefix:  "prefix-",
			Expected: `
  operator = "write"
  acl = "write"
  namespace_prefix "prefix-" {
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }`,
		},
		{
			Name:             "namespaces=enabled, consulDestNS=consul, partitions=enabled",
			EnablePartitions: true,
			PartitionName:    "part-1",
			EnableNamespaces: true,
			DestConsulNS:     "consul",
			Expected: `
partition "part-1" {
  mesh = "write"
  acl = "write"
  namespace "consul" {
    policy = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }
}`,
		},
		{
			Name:             "namespaces=enabled, mirroring=true, partitions=enabled",
			EnablePartitions: true,
			PartitionName:    "part-1",
			EnableNamespaces: true,
			Mirroring:        true,
			Expected: `
partition "part-1" {
  mesh = "write"
  acl = "write"
  namespace_prefix "" {
    policy = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }
}`,
		},
		{
			Name:             "namespaces=enabled, mirroring=true, mirroringPrefix=prefix-, partitions=enabled",
			EnablePartitions: true,
			PartitionName:    "part-1",
			EnableNamespaces: true,
			Mirroring:        true,
			MirroringPrefix:  "prefix-",
			Expected: `
partition "part-1" {
  mesh = "write"
  acl = "write"
  namespace_prefix "prefix-" {
    policy = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				flagEnableNamespaces:                 tt.EnableNamespaces,
				flagConsulInjectDestinationNamespace: tt.DestConsulNS,
				flagEnableInjectK8SNSMirroring:       tt.Mirroring,
				flagInjectK8SNSMirroringPrefix:       tt.MirroringPrefix,
				flagEnablePartitions:                 tt.EnablePartitions,
				flagPartitionName:                    tt.PartitionName,
			}

			rules, err := cmd.controllerRules()

			require.NoError(t, err)
			require.Equal(t, tt.Expected, rules)
		})
	}
}
