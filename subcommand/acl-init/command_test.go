package aclinit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAclInitClientConfig(t *testing.T) {
	config, err := renderClientACLConfig(&clientConfig{
		Secret:        "foobar",
		DefaultPolicy: "allow",
	})

	require := require.New(t)
	require.NoError(err)
	require.Equal(config.String(), strings.TrimSpace(expectedClientACLConfigTpl))
}

const expectedClientACLConfigTpl = `
{
  "acl": {
    "enabled": true,
    "default_policy": "allow",
    "down_policy": "extend-cache",
    "tokens": {
      "agent": "foobar"
    }
  }
}
`
