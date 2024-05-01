package v1alpha1

import (
	"testing"

	capi "github.com/hashicorp/consul/api"

	"github.com/stretchr/testify/require"
)

func TestToCatalogRegistration(tt *testing.T) {
	cases := map[string]struct {
		registration *Registration
		expected     *capi.CatalogRegistration
	}{
		"minimal registration": {
			registration: &Registration{
				Spec: RegistrationSpec{
					ID:         "node-id",
					Node:       "node-virtual",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			expected: &capi.CatalogRegistration{
				ID:         "node-id",
				Node:       "node-virtual",
				Address:    "127.0.0.1",
				Datacenter: "dc1",
				Service: &capi.AgentService{
					ID:      "service-id",
					Service: "service-name",
					Port:    8080,
					Address: "127.0.0.1",
				},
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		tt.Run(name, func(t *testing.T) {
			t.Parallel()
			actual := tc.registration.ToCatalogRegistration()
			require.Equal(t, tc.expected, actual)
		})
	}
}
