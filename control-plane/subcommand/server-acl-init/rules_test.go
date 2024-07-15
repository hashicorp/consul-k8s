// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
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
				consulFlags:          &flags.ConsulFlags{Partition: tt.PartitionName},
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
				consulFlags:          &flags.ConsulFlags{Partition: tt.PartitionName},
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
		EnablePeering    bool
		PartitionName    string
		Expected         string
	}{
		{
			Name: "Namespaces and peering are disabled",
			Expected: `mesh = "write"
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
			Expected: `mesh = "write"
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
		{
			Name:          "Peering is enabled with unspecified partition name (oss case)",
			EnablePeering: true,
			Expected: `mesh = "write"
peering = "read"
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
			Name:          "Peering is enabled with partition explicitly specified as default (ent default case)",
			EnablePeering: true,
			PartitionName: "default",
			Expected: `mesh = "write"
peering = "read"
partition_prefix "" {
  peering = "read"
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
			Name:          "Peering is enabled with partition explicitly specified as non-default (ent non-default case)",
			EnablePeering: true,
			PartitionName: "non-default",
			Expected: `mesh = "write"
peering = "read"
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
			Name:             "Peering and namespaces are enabled",
			EnablePeering:    true,
			EnableNamespaces: true,
			Expected: `mesh = "write"
peering = "read"
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
				flagEnablePeering:    tt.EnablePeering,
				consulFlags: &flags.ConsulFlags{
					Partition: tt.PartitionName,
				},
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
				consulFlags:          &flags.ConsulFlags{Partition: tt.PartitionName},
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
				consulFlags:          &flags.ConsulFlags{Partition: tt.PartitionName},
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
		EnablePartitions               bool
		PartitionName                  string
		EnableNamespaces               bool
		ConsulSyncDestinationNamespace string
		EnableSyncK8SNSMirroring       bool
		SyncK8SNSMirroringPrefix       string
		SyncConsulNodeName             string
		Expected                       string
	}{
		{
			Name:                           "Namespaces are disabled",
			EnablePartitions:               false,
			PartitionName:                  "",
			EnableNamespaces:               false,
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
			EnablePartitions:               false,
			PartitionName:                  "",
			EnableNamespaces:               false,
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
			EnablePartitions:               false,
			PartitionName:                  "",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       false,
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
			EnablePartitions:               false,
			PartitionName:                  "",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       false,
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
			EnablePartitions:               false,
			PartitionName:                  "",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "",
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
			EnablePartitions:               false,
			PartitionName:                  "",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "",
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
			EnablePartitions:               false,
			PartitionName:                  "",
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
			EnablePartitions:               false,
			PartitionName:                  "",
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
		{
			Name:                           "Partitions are enabled, Namespaces are enabled, mirroring disabled",
			EnablePartitions:               true,
			PartitionName:                  "foo",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       false,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "k8s-sync",
			Expected: `node "k8s-sync" {
    policy = "write"
  }
partition "foo" {
  mesh = "write"
  acl = "write"
  namespace "sync-namespace" {
    policy = "write"
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "write"
    }
  }
}`,
		},
		{
			Name:                           "Partitions are enabled, Namespaces are enabled, mirroring disabled, non-default node name",
			EnablePartitions:               true,
			PartitionName:                  "foo",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       false,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "new-node-name",
			Expected: `node "new-node-name" {
    policy = "write"
  }
partition "foo" {
  mesh = "write"
  acl = "write"
  namespace "sync-namespace" {
    policy = "write"
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "write"
    }
  }
}`,
		},
		{
			Name:                           "Partitions are enabled, Namespaces are enabled, mirroring enabled, prefix empty",
			EnablePartitions:               true,
			PartitionName:                  "foo",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "",
			SyncConsulNodeName:             "k8s-sync",
			Expected: `node "k8s-sync" {
    policy = "write"
  }
partition "foo" {
  mesh = "write"
  acl = "write"
  namespace_prefix "" {
    policy = "write"
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "write"
    }
  }
}`,
		},
		{
			Name:                           "Partitions are enabled, Namespaces are enabled, mirroring enabled, prefix empty, non-default node name",
			EnablePartitions:               true,
			PartitionName:                  "foo",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "",
			SyncConsulNodeName:             "new-node-name",
			Expected: `node "new-node-name" {
    policy = "write"
  }
partition "foo" {
  mesh = "write"
  acl = "write"
  namespace_prefix "" {
    policy = "write"
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "write"
    }
  }
}`,
		},
		{
			Name:                           "Partitions are enabled, Namespaces are enabled, mirroring enabled, prefix defined",
			EnablePartitions:               true,
			PartitionName:                  "foo",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "k8s-sync",
			Expected: `node "k8s-sync" {
    policy = "write"
  }
partition "foo" {
  mesh = "write"
  acl = "write"
  namespace_prefix "prefix-" {
    policy = "write"
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "write"
    }
  }
}`,
		},
		{
			Name:                           "Partitions are enabled, Namespaces are enabled, mirroring enabled, prefix defined, non-default node name",
			EnablePartitions:               true,
			PartitionName:                  "foo",
			EnableNamespaces:               true,
			ConsulSyncDestinationNamespace: "sync-namespace",
			EnableSyncK8SNSMirroring:       true,
			SyncK8SNSMirroringPrefix:       "prefix-",
			SyncConsulNodeName:             "new-node-name",
			Expected: `node "new-node-name" {
    policy = "write"
  }
partition "foo" {
  mesh = "write"
  acl = "write"
  namespace_prefix "prefix-" {
    policy = "write"
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "write"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				consulFlags:                        &flags.ConsulFlags{Partition: tt.PartitionName},
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
		EnablePeering    bool
		PartitionName    string
		Expected         string
	}{
		{
			EnableNamespaces: false,
			EnablePartitions: false,
			EnablePeering:    false,
			Expected: `
  mesh = "write"
  operator = "write"
  acl = "write"
  node_prefix "" {
    policy = "write"
  }
    acl = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
    identity_prefix "" {
      policy = "write"
      intentions = "write"
    }`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: false,
			EnablePeering:    false,
			Expected: `
  mesh = "write"
  operator = "write"
  acl = "write"
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    acl = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
    identity_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: false,
			EnablePeering:    true,
			Expected: `
  mesh = "write"
  operator = "write"
  acl = "write"
  peering = "write"
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    acl = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
    identity_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: true,
			EnablePeering:    false,
			PartitionName:    "part-1",
			Expected: `
partition "part-1" {
  mesh = "write"
  acl = "write"
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    policy = "write"
    acl = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
    identity_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }
}`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: true,
			EnablePeering:    true,
			PartitionName:    "part-1",
			Expected: `
partition "part-1" {
  mesh = "write"
  acl = "write"
  peering = "write"
  node_prefix "" {
    policy = "write"
  }
  namespace_prefix "" {
    policy = "write"
    acl = "write"
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
    identity_prefix "" {
      policy = "write"
      intentions = "write"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		caseName := fmt.Sprintf("ns=%t, partition=%t, peering=%t", tt.EnableNamespaces, tt.EnablePartitions, tt.EnablePeering)
		t.Run(caseName, func(t *testing.T) {

			cmd := Command{
				consulFlags:          &flags.ConsulFlags{Partition: tt.PartitionName},
				flagEnableNamespaces: tt.EnableNamespaces,
				flagEnablePeering:    tt.EnablePeering,
			}

			injectorRules, err := cmd.injectRules()

			require.NoError(t, err)
			require.Equal(t, tt.Expected, injectorRules)
		})
	}
}

// Test the dns-proxy rules with namespaces enabled or disabled.
func TestDnsProxyRules(t *testing.T) {
	cases := []struct {
		EnableNamespaces bool
		EnablePartitions bool
		EnablePeering    bool
		PartitionName    string
		Expected         string
	}{
		{
			EnableNamespaces: false,
			EnablePartitions: false,
			EnablePeering:    false,
			Expected: `
			node_prefix "" {
			  policy = "read"
			}
			service_prefix "" {
			  policy = "read"
			}`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: false,
			EnablePeering:    false,
			Expected: `
			node_prefix "" {
			  policy = "read"
			}
			service_prefix "" {
			  policy = "read"
			}`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: false,
			EnablePeering:    true,
			Expected: `
			node_prefix "" {
			  policy = "read"
			}
			service_prefix "" {
			  policy = "read"
			}`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: true,
			EnablePeering:    false,
			PartitionName:    "part-1",
			Expected: `
			partition "part-1" {
			node_prefix "" {
			  policy = "read"
			}
			service_prefix "" {
			  policy = "read"
			}
			}`,
		},
		{
			EnableNamespaces: true,
			EnablePartitions: true,
			EnablePeering:    true,
			PartitionName:    "part-1",
			Expected: `
			partition "part-1" {
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
		caseName := fmt.Sprintf("ns=%t, partition=%t, peering=%t", tt.EnableNamespaces, tt.EnablePartitions, tt.EnablePeering)
		t.Run(caseName, func(t *testing.T) {

			cmd := Command{
				consulFlags:          &flags.ConsulFlags{Partition: tt.PartitionName},
				flagEnableNamespaces: tt.EnableNamespaces,
				flagEnablePeering:    tt.EnablePeering,
			}

			injectorRules, err := cmd.dnsProxyRules()

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
      policy = "write"
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
      policy = "write"
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
      policy = "write"
      intentions = "read"
    }
  }
}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			cmd := Command{
				consulFlags:          &flags.ConsulFlags{Partition: tt.PartitionName},
				flagEnableNamespaces: tt.EnableNamespaces,
			}
			replicationTokenRules, err := cmd.aclReplicationRules()
			require.NoError(t, err)
			require.Equal(t, tt.Expected, replicationTokenRules)
		})
	}
}
