package gossipencryptionautogenerate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretGeneration(t *testing.T) {

	secret := Secret{}

	err := secret.Generate()
	require.NoError(t, err)

	secretValue := secret.Value()

	t.Logf(secretValue)
}

func TestPostToKubernetesWithNoSecretGenerated(t *testing.T) {
	secret := Secret{}

	err := secret.PostToKubernetes()
	require.Error(t, err)
}
