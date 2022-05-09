package mutatingwebhookconfiguration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestUpdateWithCABundle_emptyCertReturnsError(t *testing.T) {
	var bytes []byte
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()

	err := UpdateWithCABundle(ctx, clientset, "foo", bytes)
	require.Error(t, err, "no CA certificate in the bundle")
}

func TestUpdateWithCABundle_patchesExistingConfiguration(t *testing.T) {
	caBundleOne := []byte("ca-bundle-for-mwc")
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()

	mwc := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mwc-one",
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name: "webhook-under-test",
			},
		},
	}
	mwcCreated, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(ctx, mwc, metav1.CreateOptions{})
	require.NoError(t, err)
	err = UpdateWithCABundle(ctx, clientset, mwcCreated.Name, caBundleOne)
	mwcFetched, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, mwc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, caBundleOne, mwcFetched.Webhooks[0].ClientConfig.CABundle)
}
