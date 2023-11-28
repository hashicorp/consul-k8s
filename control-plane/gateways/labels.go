package gateways

func (b *meshGatewayBuilder) Labels() map[string]string {
	return map[string]string{
		"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
	}
}
