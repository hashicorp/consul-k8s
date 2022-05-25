package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpen(t *testing.T) {
	cases := map[string]struct{}{
		"": {},
	}

	for name, _ := range cases {
		t.Run(name, func(t *testing.T) {
			pf := &PortForward{}
			endpoint, err := pf.Open(context.Background())
			require.NoError(t, err)
			require.NotEqual(t, "", endpoint)
		})
	}
}
