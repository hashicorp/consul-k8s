// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

func TestGatewayBuilder_Annotations(t *testing.T) {
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

	b := NewGatewayBuilder[*meshv2beta1.MeshGateway](gateway, GatewayConfig{}, gatewayClassConfig, MeshGatewayAnnotationKind)

	for _, testCase := range []struct {
		Actual   map[string]string
		Expected map[string]string
	}{
		{
			Actual: b.annotationsForDeployment(),
			Expected: map[string]string{
				"gateway-annotation":            "true",
				"global-set":                    "true",
				"gateway-deployment-annotation": "true",
				"deployment-set":                "true",
			},
		},
		{
			Actual: b.annotationsForRole(),
			Expected: map[string]string{
				"gateway-annotation":      "true",
				"global-set":              "true",
				"gateway-role-annotation": "true",
				"role-set":                "true",
			},
		},
		{
			Actual: b.annotationsForRoleBinding(),
			Expected: map[string]string{
				"gateway-annotation":              "true",
				"global-set":                      "true",
				"gateway-role-binding-annotation": "true",
				"role-binding-set":                "true",
			},
		},
		{
			Actual: b.annotationsForService(),
			Expected: map[string]string{
				"gateway-annotation":         "true",
				"global-set":                 "true",
				"gateway-service-annotation": "true",
				"service-set":                "true",
			},
		},
		{
			Actual: b.annotationsForServiceAccount(),
			Expected: map[string]string{
				"gateway-annotation":                 "true",
				"global-set":                         "true",
				"gateway-service-account-annotation": "true",
				"service-account-set":                "true",
			},
		},
	} {
		assert.Equal(t, testCase.Expected, testCase.Actual)
	}
}

func TestGatewayBuilder_Labels(t *testing.T) {
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

	b := NewGatewayBuilder[*meshv2beta1.MeshGateway](gateway, GatewayConfig{}, gatewayClassConfig, MeshGatewayAnnotationKind)

	for _, testCase := range []struct {
		Actual   map[string]string
		Expected map[string]string
	}{
		{
			Actual: b.labelsForDeployment(),
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-deployment-label":             "true",
				"deployment-set":                       "true",
			},
		},
		{
			Actual: b.labelsForRole(),
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-role-label":                   "true",
				"role-set":                             "true",
			},
		},
		{
			Actual: b.labelsForRoleBinding(),
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-role-binding-label":           "true",
				"role-binding-set":                     "true",
			},
		},
		{
			Actual: b.labelsForService(),
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-service-label":                "true",
				"service-set":                          "true",
			},
		},
		{
			Actual: b.labelsForServiceAccount(),
			Expected: map[string]string{
				"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
				"gateway-label":                        "true",
				"global-set":                           "true",
				"gateway-service-account-label":        "true",
				"service-account-set":                  "true",
			},
		},
	} {
		assert.Equal(t, testCase.Expected, testCase.Actual)
	}
}

// The LogLevel for deployment containers may be set on the Gateway Class Config or the Gateway Config.
// If it is set on both, the Gateway Config takes precedence.
func TestGatewayBuilder_LogLevel(t *testing.T) {
	debug := "debug"
	info := "info"

	testCases := map[string]struct {
		GatewayLogLevel string
		GCCLogLevel     string
	}{
		"Set on Gateway": {
			GatewayLogLevel: debug,
			GCCLogLevel:     "",
		},
		"Set on GCC": {
			GatewayLogLevel: "",
			GCCLogLevel:     debug,
		},
		"Set on both": {
			GatewayLogLevel: debug,
			GCCLogLevel:     info,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			gcc := &meshv2beta1.GatewayClassConfig{
				Spec: meshv2beta1.GatewayClassConfigSpec{
					Deployment: meshv2beta1.GatewayClassDeploymentConfig{
						Container: &meshv2beta1.GatewayClassContainerConfig{
							Consul: meshv2beta1.GatewayClassConsulConfig{
								Logging: meshv2beta1.GatewayClassConsulLoggingConfig{
									Level: testCase.GCCLogLevel,
								},
							},
						},
					},
				},
			}
			b := NewGatewayBuilder(&meshv2beta1.MeshGateway{}, GatewayConfig{LogLevel: testCase.GatewayLogLevel}, gcc, MeshGatewayAnnotationKind)

			assert.Equal(t, debug, b.logLevelForDataplaneContainer())
		})
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
