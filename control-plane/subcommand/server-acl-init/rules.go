// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"bytes"
	"strings"
	"text/template"
)

type rulesData struct {
	EnablePartitions        bool
	EnablePeering           bool
	PartitionName           string
	EnableNamespaces        bool
	SyncConsulDestNS        string
	SyncEnableNSMirroring   bool
	SyncNSMirroringPrefix   string
	InjectConsulDestNS      string
	InjectEnableNSMirroring bool
	InjectNSMirroringPrefix string
	SyncConsulNodeName      string
}

type gatewayRulesData struct {
	rulesData
	GatewayName      string
	GatewayNamespace string
}

const snapshotAgentRules = `acl = "write"
key "consul-snapshot/lock" {
   policy = "write"
}
session_prefix "" {
   policy = "write"
}
service "consul-snapshot" {
   policy = "write"
}`

// The enterprise license rules are acl="write" inside partitions as operator="write"
// is unsupported in partitions.
const entLicenseRules = `operator = "write"`
const entPartitionLicenseRules = `acl = "write"`

// The partition token is utilized by the partition-init job and server-acl-init in
// non-default partitions. This token requires permissions to create partitions, read the
// agent endpoint during startup and have the ability to create an auth-method within a namespace
// for any partition.
const partitionRules = `operator = "write"
agent_prefix "" {
  policy = "read"
}
partition_prefix "" {
  namespace_prefix "" {
    acl = "write"
    service_prefix "" {
      policy = "write"
    }
  }
}`

func (c *Command) crossNamespaceRules() (string, error) {
	crossNamespaceRulesTpl := `{{- if .EnablePartitions }}
partition "{{ .PartitionName }}" {
{{- end }}
  namespace_prefix "" {
    service_prefix "" {
      policy = "read"
    }
    node_prefix "" {
      policy = "read"
    }
  }
{{- if .EnablePartitions }}
}
{{- end }}`

	return c.renderRules(crossNamespaceRulesTpl)
}

func (c *Command) agentRules() (string, error) {
	agentRulesTpl := `
{{- if .EnablePartitions }}
partition "{{ .PartitionName }}" {
{{- end }}
  node_prefix "" {
    policy = "write"
  }
{{- if .EnableNamespaces }}
  namespace_prefix "" {
{{- end }}
    service_prefix "" {
      policy = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`

	return c.renderRules(agentRulesTpl)
}

func (c *Command) anonymousTokenRules() (string, error) {
	// For Consul DNS and cross-datacenter Consul Connect,
	// the anonymous token needs to have read access to
	// services in all namespaces.
	// For Consul DNS this is needed because in a DNS request
	// no token can be presented so the anonymous policy will
	// be used and DNS needs to be able to resolve all services.
	// For cross-dc Consul Connect, each Kubernetes pod has a
	// local ACL token returned from the Kubernetes auth method.
	// When making cross-dc requests, the sidecar proxies need read
	// access to services in the other dc. When the API call
	// to read cross-dc services is forwarded to the remote dc, the
	// local ACL token is stripped and the request continues without
	// ACL token. Thus the anonymous policy must
	// allow reading all services.
	anonTokenRulesTpl := `
{{- if .EnablePartitions }}
partition_prefix "" {
{{- end }}
{{- if .EnableNamespaces }}
  namespace_prefix "" {
{{- end }}
    node_prefix "" {
       policy = "read"
    }
    service_prefix "" {
       policy = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`

	return c.renderRules(anonTokenRulesTpl)
}

// This assumes users are using the default name for the service, i.e.
// "mesh-gateway".
func (c *Command) meshGatewayRules() (string, error) {
	// Mesh gateways can only act as a proxy for services that its ACL token has access to. So, in the case of Consul
	// namespaces, it needs access to all namespaces. For peering, it requires the ability to list all peers which in
	// enterprise requires peering:read on all partitions or in OSS requires a top level peering:read. Since we cannot
	// determine whether we are using an enterprise or OSS consul image based on whether peering is enabled, we include
	// both permissions here.
	meshGatewayRulesTpl := `mesh = "write"
{{- if .EnablePeering }}
peering = "read"
{{- if eq .PartitionName "default" }}
partition_prefix "" {
  peering = "read"
}
{{- end }}
{{- end }}
{{- if .EnableNamespaces }}
namespace "default" {
{{- end }}
  service "mesh-gateway" {
     policy = "write"
  }
{{- if .EnableNamespaces }}
}
namespace_prefix "" {
{{- end }}
  node_prefix "" {
  	policy = "read"
  }
  service_prefix "" {
     policy = "read"
  }
{{- if .EnableNamespaces }}
}
{{- end }}
`

	return c.renderRules(meshGatewayRulesTpl)
}

func (c *Command) ingressGatewayRules(name, namespace string) (string, error) {
	ingressGatewayRulesTpl := `
{{- if .EnablePartitions }}
partition "{{ .PartitionName }}" {
{{- end }}
{{- if .EnableNamespaces }}
  namespace "{{ .GatewayNamespace }}" {
{{- end }}
    service "{{ .GatewayName }}" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`

	return c.renderGatewayRules(ingressGatewayRulesTpl, name, namespace)
}

// Creating a separate terminating gateway rule function because
// eventually this may need to be created with permissions for
// all of the services it represents, though that is not part
// of the initial implementation.
func (c *Command) terminatingGatewayRules(name, namespace string) (string, error) {
	terminatingGatewayRulesTpl := `
{{- if .EnablePartitions }}
partition "{{ .PartitionName }}" {
{{- end }}
{{- if .EnableNamespaces }}
  namespace "{{ .GatewayNamespace }}" {
{{- end }}
    service "{{ .GatewayName }}" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`

	return c.renderGatewayRules(terminatingGatewayRulesTpl, name, namespace)
}

// acl = "write" is required when creating namespace with a default policy.
// Attaching a default ACL policy to a namespace requires acl = "write" in the
// namespace that the policy is defined in, which in our case is "default".
func (c *Command) syncRules() (string, error) {
	syncRulesTpl := `
  node "{{ .SyncConsulNodeName }}" {
    policy = "write"
  }
{{- if .EnableNamespaces }}
{{- if .EnablePartitions }}
partition "{{ .PartitionName }}" {
  mesh = "write"
  acl = "write"
{{- else }}
  operator = "write"
  acl = "write"
{{- end }}
{{- if .SyncEnableNSMirroring }}
  namespace_prefix "{{ .SyncNSMirroringPrefix }}" {
{{- else }}
  namespace "{{ .SyncConsulDestNS }}" {
{{- end }}
{{- if .EnablePartitions }}
    policy = "write"
{{- end }}
{{- end }}
    node_prefix "" {
      policy = "read"
    }
    service_prefix "" {
      policy = "write"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`

	return c.renderRules(syncRulesTpl)
}

func (c *Command) injectRules() (string, error) {
	// The Connect injector needs permissions to create namespaces when namespaces are enabled.
	// It must also create/update service health checks via the endpoints controller.
	// When ACLs are enabled, the endpoints controller (V1) or pod controller (v2)
	// needs "acl:write" permissions to delete ACL tokens created via "consul login".
	// policy = "write" is required when creating namespaces within a partition.
	injectRulesTpl := `
{{- if .EnablePartitions }}
partition "{{ .PartitionName }}" {
  mesh = "write"
  acl = "write"
{{- else }}
  mesh = "write"
  operator = "write"
  acl = "write"
{{- end }}
{{- if .EnablePeering }}
  peering = "write"
{{- end }}
  node_prefix "" {
    policy = "write"
  }
{{- if .EnableNamespaces }}
  namespace_prefix "" {
    acl = "write"
{{- end }}
{{- if .EnablePartitions }}
    policy = "write"
{{- end }}
    service_prefix "" {
      policy = "write"
      intentions = "write"
    }
    identity_prefix "" {
      policy = "write"
      intentions = "write"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}`
	return c.renderRules(injectRulesTpl)
}

func (c *Command) aclReplicationRules() (string, error) {
	// NOTE: The node_prefix and agent_prefix rules are not required for ACL
	// replication. They're added so that this token can be used as an ACL
	// replication token, an agent token and for the server-acl-init command
	// where we need agent:read to get the current datacenter.
	// This allows us to only pass one token between
	// datacenters during federation since in order to start ACL replication,
	// we need a token with both replication and agent permissions.
	aclReplicationRulesTpl := `
{{- if .EnablePartitions }}
partition "default" {
{{- end }}
  operator = "write"
  agent_prefix "" {
    policy = "read"
  }
  node_prefix "" {
    policy = "write"
  }
{{- if .EnableNamespaces }}
  namespace_prefix "" {
{{- end }}
    acl = "write"
    service_prefix "" {
      policy = "write"
      intentions = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`
	return c.renderRules(aclReplicationRulesTpl)
}

func (c *Command) datadogAgentRules() (string, error) {
	ddAgentRulesTpl := `{{- if .EnablePartitions }}
partition "{{ .PartitionName }}" {
{{- end }}
  agent_prefix "" {
    policy = "read"
  }
  node_prefix "" {
    policy = "read"
  }
{{- if .EnableNamespaces }}
  namespace_prefix "" {
{{- end }}
    service_prefix "" {
      policy = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`
	return c.renderRules(ddAgentRulesTpl)
}

func (c *Command) rulesData() rulesData {
	return rulesData{
		EnablePartitions:        c.consulFlags.Partition != "",
		EnablePeering:           c.flagEnablePeering,
		PartitionName:           c.consulFlags.Partition,
		EnableNamespaces:        c.flagEnableNamespaces,
		SyncConsulDestNS:        c.flagConsulSyncDestinationNamespace,
		SyncEnableNSMirroring:   c.flagEnableSyncK8SNSMirroring,
		SyncNSMirroringPrefix:   c.flagSyncK8SNSMirroringPrefix,
		InjectConsulDestNS:      c.flagConsulInjectDestinationNamespace,
		InjectEnableNSMirroring: c.flagEnableInjectK8SNSMirroring,
		InjectNSMirroringPrefix: c.flagInjectK8SNSMirroringPrefix,
		SyncConsulNodeName:      c.flagSyncConsulNodeName,
	}
}

func (c *Command) renderRules(tmpl string) (string, error) {
	return c.renderRulesGeneric(tmpl, c.rulesData())
}

func (c *Command) renderGatewayRules(tmpl, gatewayName, gatewayNamespace string) (string, error) {
	// Populate the data that will be used in the template.
	// Not all templates will need all of the fields.
	data := gatewayRulesData{
		rulesData:        c.rulesData(),
		GatewayName:      gatewayName,
		GatewayNamespace: gatewayNamespace,
	}

	return c.renderRulesGeneric(tmpl, data)
}

func (c *Command) renderRulesGeneric(tmpl string, data interface{}) (string, error) {
	// Check that it's a valid template
	compiled, err := template.New("root").Parse(strings.TrimSpace(tmpl))
	if err != nil {
		return "", err
	}

	// Render the template
	var buf bytes.Buffer
	err = compiled.Execute(&buf, data)
	if err != nil {
		// Discard possible partial results on error return
		return "", err
	}

	return buf.String(), nil
}
