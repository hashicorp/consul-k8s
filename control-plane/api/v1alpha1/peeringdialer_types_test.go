package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPeeringDialer_Validate(t *testing.T) {
	cases := map[string]struct {
		dialer          *PeeringDialer
		expectedErrMsgs []string
	}{
		"valid": {
			dialer: &PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: PeeringDialerSpec{
					Peer: &Peer{
						Secret: &Secret{
							Name:    "api-token",
							Key:     "data",
							Backend: SecretBackendTypeKubernetes,
						},
					},
				},
			},
		},
		"no peer specified": {
			dialer: &PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: PeeringDialerSpec{},
			},
			expectedErrMsgs: []string{
				`spec.peer: Invalid value: "null": peer must be specified`,
			},
		},
		"no secret specified": {
			dialer: &PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: PeeringDialerSpec{
					Peer: &Peer{},
				},
			},
			expectedErrMsgs: []string{
				`spec.peer.secret: Invalid value: "null": secret must be specified`,
			},
		},
		"invalid secret backend": {
			dialer: &PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "api",
				},
				Spec: PeeringDialerSpec{
					Peer: &Peer{
						Secret: &Secret{
							Backend: "invalid",
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.peer.secret.backend: Invalid value: "invalid": backend must be "kubernetes"`,
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.dialer.Validate()
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
