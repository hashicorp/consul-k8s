package gateways

import (
	"testing"

	"github.com/stretchr/testify/assert"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

func Test_computeAnnotationsOrLabels(t *testing.T) {
	gatewaySet := map[string]string{
		"service.beta.kubernetes.io/aws-load-balancer-name": "my-lb", // Will be inherited
	}

	primary := meshv2beta1.GatewayClassAnnotationsLabelsConfig{
		InheritFromGateway: []string{
			"service.beta.kubernetes.io/aws-load-balancer-name",
		},
		Set: map[string]string{
			"created-by":  "nathancoleman",             // Only exists in primary
			"owning-team": "consul-gateway-management", // Will override secondary
		},
	}

	secondary := meshv2beta1.GatewayClassAnnotationsLabelsConfig{
		InheritFromGateway: []string{},
		Set: map[string]string{
			"created-on":  "kubernetes", // Only exists in secondary
			"owning-team": "consul",     // Will be overridden by primary
		},
	}

	actual := computeAnnotationsOrLabels(gatewaySet, primary, secondary)
	expected := map[string]string{
		"created-by":  "nathancoleman",             // Set by primary
		"created-on":  "kubernetes",                // Set by secondary
		"owning-team": "consul-gateway-management", // Set by primary, overrode secondary
		"service.beta.kubernetes.io/aws-load-balancer-name": "my-lb", // Inherited from gateway
	}

	assert.Equal(t, expected, actual)
}
