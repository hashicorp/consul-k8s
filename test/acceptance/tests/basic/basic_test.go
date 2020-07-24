package basic

import (
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

// Test that the basic installation, i.e. just
// servers and clients, works by creating a kv entry
// and subsequently reading it from Consul.
func TestBasicInstallation(t *testing.T) {
	cases := []struct {
		name       string
		helmValues map[string]string
		secure     bool
	}{
		{
			"Default installation",
			nil,
			false,
		},
		{
			"Secure installation (with TLS and ACLs enabled)",
			map[string]string{
				"global.tls.enabled":           "true",
				"global.acls.manageSystemACLs": "true",
			},
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, c.helmValues, suite.Environment().DefaultContext(t), suite.Config(), releaseName)

			consulCluster.Create(t)

			client := consulCluster.SetupConsulClient(t, c.secure)

			// Create a KV entry
			randomKey := helpers.RandomName()
			randomValue := []byte(helpers.RandomName())
			t.Logf("creating KV entry with key %s", randomKey)
			_, err := client.KV().Put(&api.KVPair{
				Key:   randomKey,
				Value: randomValue,
			}, nil)
			require.NoError(t, err)

			t.Logf("reading value for key %s", randomKey)
			kv, _, err := client.KV().Get(randomKey, nil)
			require.NoError(t, err)
			require.Equal(t, kv.Value, randomValue)
		})
	}
}
