package serveraclinit

import (
	"bytes"
	"strings"
	"text/template"
)

type rulesData struct {
	EnableNamespaces               bool
	ConsulSyncDestinationNamespace string
	EnableSyncK8SNSMirroring       bool
	SyncK8SNSMirroringPrefix       string
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

const entLicenseRules = `operator = "write"`

const crossNamespaceRules = `namespace_prefix "" {
  service_prefix "" {
    policy = "read"
  }
  node_prefix "" {
    policy = "read"
  }
} `

func (c *Command) agentRules() (string, error) {
	agentRulesTpl := `
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
`

	return c.renderRules(anonTokenRulesTpl)
}

// This assumes users are using the default name for the service, i.e.
// "mesh-gateway".
func (c *Command) meshGatewayRules() (string, error) {
	// Mesh gateways can only act as a proxy for services
	// that its ACL token has access to. So, in the case of
	// Consul namespaces, it needs access to all namespaces.
	meshGatewayRulesTpl := `
  agent_prefix "" {
  	policy = "read"
  }
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

func (c *Command) syncRules() (string, error) {
	syncRulesTpl := `
  node "k8s-sync" {
    policy = "write"
  }
{{- if .EnableNamespaces }}
operator = "write"
{{- if .EnableSyncK8SNSMirroring }}
namespace_prefix "{{ .SyncK8SNSMirroringPrefix }}" {
{{- else }}
namespace "{{ .ConsulSyncDestinationNamespace }}" {
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
`

	return c.renderRules(syncRulesTpl)
}

// This should only be set when namespaces are enabled.
func (c *Command) injectRules() (string, error) {
	// The Connect injector only needs permissions to create namespaces
	injectRulesTpl := `
{{- if .EnableNamespaces }}
operator = "write"
{{- end }}
`

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
acl = "write"
operator = "write"
agent_prefix "" {
  policy = "read"
}
{{- if .EnableNamespaces }}
namespace_prefix "" {
{{- end }}
  node_prefix "" {
    policy = "write"
  }
  service_prefix "" {
    policy = "read"
    intentions = "read"
  }
{{- if .EnableNamespaces }}
}
{{- end }}
`
	return c.renderRules(aclReplicationRulesTpl)
}

func (c *Command) renderRules(tmpl string) (string, error) {
	// Check that it's a valid template
	compiled, err := template.New("root").Parse(strings.TrimSpace(tmpl))
	if err != nil {
		return "", err
	}

	// Populate the data that will be used in the template.
	// Not all templates will need all of the fields.
	data := rulesData{
		EnableNamespaces:               c.flagEnableNamespaces,
		ConsulSyncDestinationNamespace: c.flagConsulSyncDestinationNamespace,
		EnableSyncK8SNSMirroring:       c.flagEnableSyncK8SNSMirroring,
		SyncK8SNSMirroringPrefix:       c.flagSyncK8SNSMirroringPrefix,
	}

	// Render the template
	var buf bytes.Buffer
	err = compiled.Execute(&buf, &data)
	if err != nil {
		// Discard possible partial results on error return
		return "", err
	}

	return buf.String(), nil
}
