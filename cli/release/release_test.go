package release

import (
	"testing"

	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/stretchr/testify/require"
)

func TestShouldExpectFederationSecret(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		configuration helm.Values
		expected      bool
	}{
		"Primary DC, no federation": {
			configuration: helm.Values{
				Global: helm.Global{
					Datacenter: "dc1",
				},
			},
			expected: false,
		},
		"Primary DC, federation enabled": {
			configuration: helm.Values{

				Global: helm.Global{
					Datacenter: "dc1",
					Federation: helm.Federation{
						Enabled:           true,
						PrimaryDatacenter: "dc1",
					},
				},
			},
			expected: false,
		},
		"Non-primary DC, federation enabled": {
			configuration: helm.Values{
				Global: helm.Global{
					Datacenter: "dc2",
					Federation: helm.Federation{
						Enabled:                true,
						PrimaryDatacenter:      "dc1",
						CreateFederationSecret: false,
					},
				},
			},
			expected: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			release := Release{
				Configuration: tc.configuration,
			}

			actual := release.ShouldExpectFederationSecret()
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestFedSecret(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		configuration helm.Values
		expected      string
	}{
		"No Fed Secret": {
			configuration: helm.Values{},
			expected:      "",
		},
		"Secret in Global.TLS.CaCert.SecretName": {
			configuration: helm.Values{
				Global: helm.Global{
					TLS: helm.TLS{
						CaCert: helm.CaCert{
							SecretName: "secret-name",
						},
					},
				},
			},
			expected: "secret-name",
		},
		"Secret in Global.TLS.CaKey.SecretName": {
			configuration: helm.Values{
				Global: helm.Global{
					TLS: helm.TLS{
						CaKey: helm.CaKey{
							SecretName: "secret-name",
						},
					},
				},
			},
			expected: "secret-name",
		},
		"Secret in Global.Acls.ReplicationToken.SecretName": {
			configuration: helm.Values{
				Global: helm.Global{
					Acls: helm.Acls{
						ReplicationToken: helm.ReplicationToken{
							SecretName: "secret-name",
						},
					},
				},
			},
			expected: "secret-name",
		},
		"Secret in Global.GossipEncryption.SecretName": {
			configuration: helm.Values{
				Global: helm.Global{
					GossipEncryption: helm.GossipEncryption{
						SecretName: "secret-name",
					},
				},
			},
			expected: "secret-name",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			release := Release{
				Configuration: tc.configuration,
			}

			actual := release.FedSecret()
			require.Equal(t, tc.expected, actual)
		})
	}

}
