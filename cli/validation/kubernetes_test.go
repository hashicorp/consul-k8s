package validation

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListConsulSecrets(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		secrets         *v1.SecretList
		namespace       string
		expectedSecrets int
	}{
		"No secrets": {
			secrets:         &v1.SecretList{},
			expectedSecrets: 0,
		},
		"A Consul Secret": {
			secrets: &v1.SecretList{
				Items: []v1.Secret{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "test-consul-bootstrap-acl-token",
							Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
						},
					},
				},
			},
			namespace:       v1.NamespaceDefault,
			expectedSecrets: 1,
		},
		"A Consul and a non-Consul Secret": {
			secrets: &v1.SecretList{
				Items: []v1.Secret{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "test-consul-bootstrap-acl-token",
							Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "not-a-consul-secret",
						},
					},
				},
			},
			namespace:       v1.NamespaceDefault,
			expectedSecrets: 1,
		},
		"A Consul Secret in default namespace with lookup in consul namespace": {
			secrets: &v1.SecretList{
				Items: []v1.Secret{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "test-consul-bootstrap-acl-token",
							Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
						},
					},
				},
			},
			namespace:       "consul",
			expectedSecrets: 0,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()

			for _, secret := range tc.secrets.Items {
				_, err := client.CoreV1().Secrets(v1.NamespaceDefault).Create(context.Background(), &secret, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			actual, err := ListConsulSecrets(context.Background(), client, tc.namespace)
			require.NoError(t, err)
			require.Equal(t, tc.expectedSecrets, len(actual.Items))
		})
	}
}
