package serveraclinit

import (
	"bytes"
	"strings"
	"text/template"
)

type rulesData struct {
	EnableNamespaces        bool
	SyncConsulDestNS        string
	SyncEnableNSMirroring   bool
	SyncNSMirroringPrefix   string
	InjectConsulDestNS      string
	InjectEnableNSMirroring bool
	InjectNSMirroringPrefix string
	SyncConsulNodeName      string
	EnableHealthChecks      bool
	EnableCleanupController bool
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

func (c *Command) ingressGatewayRules(name, namespace string) (string, error) {
	ingressGatewayRulesTpl := `
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
`

	return c.renderGatewayRules(ingressGatewayRulesTpl, name, namespace)
}

// Creating a separate terminating gateway rule function because
// eventually this may need to be created with permissions for
// all of the services it represents, though that is not part
// of the initial implementation
func (c *Command) terminatingGatewayRules(name, namespace string) (string, error) {
	terminatingGatewayRulesTpl := `
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
`

	return c.renderGatewayRules(terminatingGatewayRulesTpl, name, namespace)
}

func (c *Command) syncRules() (string, error) {
	syncRulesTpl := `
  node "{{ .SyncConsulNodeName }}" {
    policy = "write"
  }
{{- if .EnableNamespaces }}
operator = "write"
{{- if .SyncEnableNSMirroring }}
namespace_prefix "{{ .SyncNSMirroringPrefix }}" {
{{- else }}
namespace "{{ .SyncConsulDestNS }}" {
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

func (c *Command) injectRules() (string, error) {
	// The Connect injector needs permissions to create namespaces when namespaces are enabled.
	// If health checks are enabled it must also create/update service checks.
	// If the cleanup controller is enabled, it must be able to delete service
	// instances from every client.
	injectRulesTpl := `
{{- if .EnableNamespaces }}
operator = "write"
{{- end }}
{{- if (or .EnableHealthChecks .EnableCleanupController) }}
node_prefix "" {
  policy = "write"
}
{{- if .EnableNamespaces }}
namespace_prefix "" {
{{- end }}
  acl = "write"
  service_prefix "" {
    policy = "write"
  }
{{- if .EnableNamespaces }}
}
{{- end }}
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
    policy = "read"
    intentions = "read"
  }
{{- if .EnableNamespaces }}
}
{{- end }}
`
	return c.renderRules(aclReplicationRulesTpl)
}

func (c *Command) controllerRules() (string, error) {
	controllerRules := `
operator = "write"
{{- if .EnableNamespaces }}
{{- if .InjectEnableNSMirroring }}
namespace_prefix "{{ .InjectNSMirroringPrefix }}" {
{{- else }}
namespace "{{ .InjectConsulDestNS }}" {
{{- end }}
{{- end }}
  service_prefix "" {
    policy = "write"
    intentions = "write"
  }
{{- if .EnableNamespaces }}
}
{{- end }}
`
	return c.renderRules(controllerRules)
}

func (c *Command) rulesData() rulesData {
	return rulesData{
		EnableNamespaces:        c.flagEnableNamespaces,
		SyncConsulDestNS:        c.flagConsulSyncDestinationNamespace,
		SyncEnableNSMirroring:   c.flagEnableSyncK8SNSMirroring,
		SyncNSMirroringPrefix:   c.flagSyncK8SNSMirroringPrefix,
		InjectConsulDestNS:      c.flagConsulInjectDestinationNamespace,
		InjectEnableNSMirroring: c.flagEnableInjectK8SNSMirroring,
		InjectNSMirroringPrefix: c.flagInjectK8SNSMirroringPrefix,
		SyncConsulNodeName:      c.flagSyncConsulNodeName,
		EnableHealthChecks:      c.flagEnableHealthChecks,
		EnableCleanupController: c.flagEnableCleanupController,
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
