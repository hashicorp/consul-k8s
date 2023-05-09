package translation

import (
	"context"
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"github.com/google/go-cmp/cmp"

	"github.com/hashicorp/consul/api"
)

func TestTranslateConsulGateway(t *testing.T) {
	type args struct {
		config *api.APIGatewayConfigEntry
	}
	tests := []struct {
		name string
		args args
		want []types.NamespacedName
	}{
		{
			name: "when name and namespace are set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						metaKeyKubeNS:   "my-ns",
						metaKeyKubeName: "api-gw-name",
					},
				},
			},
			want: []types.NamespacedName{
				{
					Namespace: "my-ns",
					Name:      "api-gw-name",
				},
			},
		},
		{
			name: "when name is not set and namespace is set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						metaKeyKubeNS: "my-ns",
					},
				},
			},
			want: nil,
		},
		{
			name: "when name is set and namespace is not set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						metaKeyKubeName: "api-gw-name",
					},
				},
			},
			want: nil,
		},
		{
			name: "when both name and namespace are not set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{},
				},
			},
			want: nil,
		},
	}

	transformer := cmp.Transformer("Sort", func(in []types.NamespacedName) []types.NamespacedName {
		sort.Slice(in, func(i int, j int) bool {
			return in[i].Name < in[j].Name
		})
		return in
	})
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fn := TranslateConsulGateway(context.Background())
			got := fn(tt.args.config)
			if diff := cmp.Diff(got, tt.want, transformer); diff != "" {
				t.Errorf("TranslateConsulGateway() %s", diff)
			}
		})
	}
}
