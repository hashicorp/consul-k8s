package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTerminatingGatewayService_Validate(t *testing.T) {
	cases := map[string]struct {
		terminatingGatewayService *TerminatingGatewayService
		expectedErrMsgs           []string
	}{
		"valid": {
			terminatingGatewayService: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: TerminatingGatewayServiceSpec{
					Service: &CatalogService{
						Node:        "legacy_node",
						ServiceName: "test-service",
					},
				},
			},
		},
		"no service specified": {
			terminatingGatewayService: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: TerminatingGatewayServiceSpec{},
			},
			expectedErrMsgs: []string{
				`spec.service: Invalid value: "null": service must be specified`,
			},
		},
		"no node specified": {
			terminatingGatewayService: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: TerminatingGatewayServiceSpec{
					Service: &CatalogService{
						ServiceName: "test-service",
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.service.node: Invalid value: "": node must be specified`,
			},
		},
		"no serviceName specified": {
			terminatingGatewayService: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: TerminatingGatewayServiceSpec{
					Service: &CatalogService{
						Node:        "legacy-node",
						ServiceName: "",
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.service.serviceName: Invalid value: "": serviceName must be specified`,
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.terminatingGatewayService.Validate()
			if len(testCase.expectedErrMsgs) != 0 {
				require.Error(t, err)
				for _, s := range testCase.expectedErrMsgs {
					require.Contains(t, err.Error(), s)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
