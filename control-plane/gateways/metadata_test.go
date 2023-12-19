package gateways

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

func TestMeshGatewayBuilder_Annotations(t *testing.T) {
	gateway := &meshv2beta1.MeshGateway{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"gateway-annotation":                 "true", // Will be inherited by all resources
				"gateway-deployment-annotation":      "true", // Will be inherited by Deployment
				"gateway-role-annotation":            "true", // Will be inherited by Role
				"gateway-role-binding-annotation":    "true", // Will be inherited by RoleBinding
				"gateway-service-annotation":         "true", // Will be inherited by Service
				"gateway-service-account-annotation": "true", // Will be inherited by ServiceAccount
			},
		},
	}

	gatewayClassConfig := &meshv2beta1.GatewayClassConfig{
		Spec: meshv2beta1.GatewayClassConfigSpec{
			GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
				Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
					InheritFromGateway: []string{"gateway-annotation"},
					Set:                map[string]string{"global-set": "true"},
				},
			},
			Deployment: meshv2beta1.GatewayClassDeploymentConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-deployment-annotation"},
						Set:                map[string]string{"deployment-set": "true"},
					},
				},
			},
			Role: meshv2beta1.GatewayClassRoleConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-role-annotation"},
						Set:                map[string]string{"role-set": "true"},
					},
				},
			},
			RoleBinding: meshv2beta1.GatewayClassRoleBindingConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-role-binding-annotation"},
						Set:                map[string]string{"role-binding-set": "true"},
					},
				},
			},
			Service: meshv2beta1.GatewayClassServiceConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-service-annotation"},
						Set:                map[string]string{"service-set": "true"},
					},
				},
			},
			ServiceAccount: meshv2beta1.GatewayClassServiceAccountConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Annotations: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-service-account-annotation"},
						Set:                map[string]string{"service-account-set": "true"},
					},
				},
			},
		},
	}

	b := NewMeshGatewayBuilder(gateway, GatewayConfig{}, gatewayClassConfig)

	for _, testCase := range []struct {
		Object   client.Object
		Expected map[string]string
	}{
		{
			// Unhandled type should return empty set
			Object:   &corev1.Namespace{},
			Expected: map[string]string{},
		},
		{
			Object: &appsv1.Deployment{},
			Expected: map[string]string{
				"gateway-annotation":            "true",
				"global-set":                    "true",
				"gateway-deployment-annotation": "true",
				"deployment-set":                "true",
			},
		},
		{
			Object: &rbacv1.Role{},
			Expected: map[string]string{
				"gateway-annotation":      "true",
				"global-set":              "true",
				"gateway-role-annotation": "true",
				"role-set":                "true",
			},
		},
		{
			Object: &rbacv1.RoleBinding{},
			Expected: map[string]string{
				"gateway-annotation":              "true",
				"global-set":                      "true",
				"gateway-role-binding-annotation": "true",
				"role-binding-set":                "true",
			},
		},
		{
			Object: &corev1.Service{},
			Expected: map[string]string{
				"gateway-annotation":         "true",
				"global-set":                 "true",
				"gateway-service-annotation": "true",
				"service-set":                "true",
			},
		},
		{
			Object: &corev1.ServiceAccount{},
			Expected: map[string]string{
				"gateway-annotation":                 "true",
				"global-set":                         "true",
				"gateway-service-account-annotation": "true",
				"service-account-set":                "true",
			},
		},
	} {
		actual := b.Annotations(testCase.Object)
		assert.Equal(t, testCase.Expected, actual)
	}
}

func TestNewMeshGatewayBuilder_Labels(t *testing.T) {
	gateway := &meshv2beta1.MeshGateway{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"gateway-label":                 "true", // Will be inherited by all resources
				"gateway-deployment-label":      "true", // Will be inherited by Deployment
				"gateway-role-label":            "true", // Will be inherited by Role
				"gateway-role-binding-label":    "true", // Will be inherited by RoleBinding
				"gateway-service-label":         "true", // Will be inherited by Service
				"gateway-service-account-label": "true", // Will be inherited by ServiceAccount
			},
		},
	}

	gatewayClassConfig := &meshv2beta1.GatewayClassConfig{
		Spec: meshv2beta1.GatewayClassConfigSpec{
			GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
				Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
					InheritFromGateway: []string{"gateway-label"},
					Set:                map[string]string{"global-set": "true"},
				},
			},
			Deployment: meshv2beta1.GatewayClassDeploymentConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-deployment-label"},
						Set:                map[string]string{"deployment-set": "true"},
					},
				},
			},
			Role: meshv2beta1.GatewayClassRoleConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-role-label"},
						Set:                map[string]string{"role-set": "true"},
					},
				},
			},
			RoleBinding: meshv2beta1.GatewayClassRoleBindingConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-role-binding-label"},
						Set:                map[string]string{"role-binding-set": "true"},
					},
				},
			},
			Service: meshv2beta1.GatewayClassServiceConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-service-label"},
						Set:                map[string]string{"service-set": "true"},
					},
				},
			},
			ServiceAccount: meshv2beta1.GatewayClassServiceAccountConfig{
				GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
					Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
						InheritFromGateway: []string{"gateway-service-account-label"},
						Set:                map[string]string{"service-account-set": "true"},
					},
				},
			},
		},
	}

	b := NewMeshGatewayBuilder(gateway, GatewayConfig{}, gatewayClassConfig)

	for _, testCase := range []struct {
		Object   client.Object
		Expected map[string]string
	}{
		{
			// Unhandled type should return default set
			Object:   &corev1.Namespace{},
			Expected: defaultLabels,
		},
		{
			Object: &appsv1.Deployment{},
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-deployment-label":             "true",
				"deployment-set":                       "true",
			},
		},
		{
			Object: &rbacv1.Role{},
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-role-label":                   "true",
				"role-set":                             "true",
			},
		},
		{
			Object: &rbacv1.RoleBinding{},
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-role-binding-label":           "true",
				"role-binding-set":                     "true",
			},
		},
		{
			Object: &corev1.Service{},
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-service-label":                "true",
				"service-set":                          "true",
			},
		},
		{
			Object: &corev1.ServiceAccount{},
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-service-account-label":        "true",
				"service-account-set":                  "true",
			},
		},
	} {
		actual := b.Labels(testCase.Object)
		assert.Equal(t, testCase.Expected, actual)
	}
}

func Test_computeAnnotationsOrLabels(t *testing.T) {
	gatewaySet := map[string]string{
		"service.beta.kubernetes.io/aws-load-balancer-internal": "true",  // Will not be inherited
		"service.beta.kubernetes.io/aws-load-balancer-name":     "my-lb", // Will be inherited
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
