package flags

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Taken from https://github.com/hashicorp/consul/blob/b5b9c8d953cd3c79c6b795946839f4cf5012f507/command/flags/http_test.go
// This was done so we don't depend on internal Consul implementation.

func TestHTTPFlagsSetToken(t *testing.T) {
	var f HTTPFlags
	require := require.New(t)
	require.Empty(f.Token())
	require.NoError(f.SetToken("foo"))
	require.Equal("foo", f.Token())
}
