package serveraclinit

import (
	"bytes"
	"strings"
	"text/template"
)

type rulesData struct {
	EnableNamespaces      bool
	ConsulSyncNamespace   string
	EnableSyncNSMirroring bool
	SyncMirroringPrefix   string
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

func (c *Command) dnsRules() (string, error) {
	// DNS rules need to have access to all namespaces
	// to be able to resolve services in any namespace.
	dnsRulesTpl := `
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

	return c.renderRules(dnsRulesTpl)
}

// This assumes users are using the default name for the service, i.e.
// "mesh-gateway".
func (c *Command) meshGatewayRules() (string, error) {
	meshGatewayRulesTpl := `
{{- if .EnableNamespaces }}
namespace_prefix "" {
{{- end }}
  service_prefix "" {
     policy = "read"
  }

  service "mesh-gateway" {
     policy = "write"
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
{{- if .EnableSyncNSMirroring }}
namespace_prefix "{{ .SyncMirroringPrefix }}" {
{{- else }}
namespace "{{ .ConsulSyncNamespace }}" {
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

func (c *Command) renderRules(tmpl string) (string, error) {
	// Check that it's a valid template
	compiled, err := template.New("root").Parse(strings.TrimSpace(tmpl))
	if err != nil {
		return "", err
	}

	// Populate the data that will be used in the template.
	// Not all templates will need all of the fields.
	data := rulesData{
		EnableNamespaces:      c.flagEnableNamespaces,
		ConsulSyncNamespace:   c.flagConsulSyncNamespace,
		EnableSyncNSMirroring: c.flagEnableSyncNSMirroring,
		SyncMirroringPrefix:   c.flagSyncMirroringPrefix,
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
