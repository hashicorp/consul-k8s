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
