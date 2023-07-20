package godiscover

import (
	"errors"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/helper/go-discover/mocks"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestConsulServerAddresses(t *testing.T) {
	logger := hclog.New(nil)

	tests := []struct {
		name            string
		discoverString  string
		want            []string
		wantErr         bool
		errMessage      string
		wantProviderErr bool
	}{
		{
			"Gets addresses from the provider",
			"provider=mock",
			[]string{"1.1.1.1", "2.2.2.2"},
			false,
			"",
			false,
		},
		{
			"Errors when no addresses were discovered",
			"provider=mock",
			nil,
			true,
			"could not discover any Consul servers with \"provider=mock\"",
			false,
		},
		{
			"Errors when the the provider errors",
			"provider=mock",
			nil,
			true,
			"provider error",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := mocks.MockProvider{}
			providers := map[string]discover.Provider{
				"mock": &provider,
			}
			if tt.wantProviderErr {
				provider.On("Addrs", mock.Anything, mock.Anything).Return(nil, errors.New(tt.errMessage))
			} else {
				provider.On("Addrs", mock.Anything, mock.Anything).Return(tt.want, nil)
			}
			got, err := ConsulServerAddresses(tt.discoverString, providers, logger)
			if !tt.wantErr {
				require.Equal(t, tt.want, got)
			} else {
				require.Error(t, err)
				require.EqualError(t, err, tt.errMessage)
			}
		})
	}
}
