package gossipencryptionautogenerate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {

	secret := Secret{}

	err := secret.Generate()
	require.NoError(t, err)

	secretValue := secret.Value()

	t.Logf(secretValue)
}

func TestWrite_WithNoSecretGenerated(t *testing.T) {
	secret := Secret{}

	err := secret.Write(nil)
	require.Error(t, err)
}
