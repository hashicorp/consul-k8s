package gateways

import (
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/pointer"
	"testing"
)

func Test_meshGatewayBuilder_Deployment(t *testing.T) {
	type fields struct {
		gateway *meshv2beta1.MeshGateway
		config  common.GatewayConfig
		gcc     *meshv2beta1.GatewayClassConfig
	}
	tests := []struct {
		name    string
		fields  fields
		want    *appsv1.Deployment
		wantErr bool
	}{
		{
			name: "happy path",
			fields: fields{
				gateway: &meshv2beta1.MeshGateway{
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
					},
				},
				config: common.GatewayConfig{
					ImageDataplane:             "",
					ImageConsulK8S:             "",
					ConsulDestinationNamespace: "",
					NamespaceMirroringPrefix:   "",
					EnableNamespaces:           false,
					EnableNamespaceMirroring:   false,
					AuthMethod:                 "",
					LogLevel:                   "",
					ConsulPartition:            "",
					LogJSON:                    false,
					TLSEnabled:                 false,
					PeeringEnabled:             false,
					ConsulTLSServerName:        "",
					ConsulCACert:               "",
					ConsulConfig:               common.ConsulConfig{},
					EnableOpenShift:            false,
					MapPrivilegedServicePorts:  0,
				},
				gcc: &meshv2beta1.GatewayClassConfig{
					Spec: pbmesh.GatewayClassConfig{
						Deployment: &pbmesh.Deployment{
							DefaultInstances: pointer.Uint32(1),
							MinInstances:     pointer.Uint32(1),
							MaxInstances:     pointer.Uint32(8),
						},
					},
				},
			},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &meshGatewayBuilder{
				gateway: tt.fields.gateway,
				config:  tt.fields.config,
				gcc:     tt.fields.gcc,
			}
			got, err := b.Deployment()
			if !tt.wantErr && (err != nil) {
				assert.Errorf(t, err, "Error")
			}
			assert.Equalf(t, tt.want, got, "Deployment()")
		})
	}
}
