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
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "test-service",
						},
					},
				},
			},
		},
		"no catalogRegistration specified": {
			terminatingGatewayService: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: TerminatingGatewayServiceSpec{},
			},
			expectedErrMsgs: []string{
				`spec.catalogRegistration: Invalid value: "null": catalogRegistration must be specified`,
			},
		},
		"no node specified": {
			terminatingGatewayService: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "foo",
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.catalogRegistration.node: Invalid value: "": node must be specified`,
			},
		},
		"no service specified": {
			terminatingGatewayService: &TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: TerminatingGatewayServiceSpec{
					CatalogRegistration: &CatalogRegistration{
						Node:    "legacy_node",
						Address: "10.20.10.22",
						Service: AgentService{
							Service: "",
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.catalogRegistration.service.service: Invalid value: "": service must be specified`,
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
