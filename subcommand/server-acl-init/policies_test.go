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
			`
  service_prefix "" {
     policy = "read"
  }

  service "mesh-gateway" {
     policy = "write"
  }`,
		},
		{
			"Namespaces are enabled",
			true,
			`
namespace_prefix "" {
  service_prefix "" {
     policy = "read"
  }

  service "mesh-gateway" {
     policy = "write"
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
		Name                string
		EnableNamespaces    bool
		ConsulSyncNamespace string
		EnableNSMirroring   bool
		MirroringPrefix     string
		Expected            string
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
				flagEnableNamespaces:    tt.EnableNamespaces,
				flagConsulSyncNamespace: tt.ConsulSyncNamespace,
				flagEnableNSMirroring:   tt.EnableNSMirroring,
				flagMirroringPrefix:     tt.MirroringPrefix,
			}

			syncRules, err := cmd.syncRules()

			require.NoError(err)
			require.Equal(tt.Expected, syncRules)
		})
	}
}

func TestInjectorRules(t *testing.T) {
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
