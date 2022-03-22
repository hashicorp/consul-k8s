package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsConsulImage(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		image string
		valid bool
	}{
		"Not a Consul image": {
			image: "not-a-consul-image",
			valid: false,
		},
		"Valid Consul image": {
			image: "hashicorp/consul:1.0.0",
			valid: true,
		},
		"Valid Consul Enterprise image": {
			image: "hashicorp/consul-enterprise:1.10.0-ent",
			valid: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			valid := IsConsulImage(tc.image)
			require.Equal(t, tc.valid, valid)
		})
	}
}

func TestIsConsulEnterpriseImage(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		image string
		valid bool
	}{
		"Not a Consul image": {
			image: "not-a-consul-image",
			valid: false,
		},
		"Valid Consul image": {
			image: "hashicorp/consul:1.0.0",
			valid: false,
		},
		"Valid Consul Enterprise image": {
			image: "hashicorp/consul-enterprise:1.10.0-ent",
			valid: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			valid := IsConsulEnterpriseImage(tc.image)
			require.Equal(t, tc.valid, valid)
		})
	}
}
