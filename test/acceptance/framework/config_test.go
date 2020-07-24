package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_HelmValuesFromConfig(t *testing.T) {
	type fields struct {
		ConsulImage    string
		ConsulK8SImage string
	}
	tests := []struct {
		name   string
		fields fields
		want   map[string]string
	}{
		{
			"returns empty map by default",
			fields{},
			map[string]string{},
		},
		{
			"sets consul image",
			fields{
				ConsulImage: "consul:test-version",
			},
			map[string]string{"global.image": "consul:test-version"},
		},
		{
			"sets consul-k8s image",
			fields{
				ConsulK8SImage: "consul-k8s:test-version",
			},
			map[string]string{"global.imageK8S": "consul-k8s:test-version"},
		},
		{
			"sets both images",
			fields{
				ConsulImage:    "consul:test-version",
				ConsulK8SImage: "consul-k8s:test-version",
			},
			map[string]string{
				"global.image":    "consul:test-version",
				"global.imageK8S": "consul-k8s:test-version",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &TestConfig{
				ConsulImage:    tt.fields.ConsulImage,
				ConsulK8SImage: tt.fields.ConsulK8SImage,
			}
			require.Equal(t, cfg.HelmValuesFromConfig(), tt.want)
		})
	}
}
