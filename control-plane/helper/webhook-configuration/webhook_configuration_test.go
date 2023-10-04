// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookconfiguration

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
	require.NoError(t, err)
	mwcFetched, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, mwc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, caBundleOne, mwcFetched.Webhooks[0].ClientConfig.CABundle)
}

func TestUpdateWithCABundle_patchesExistingConfigurationForValidating(t *testing.T) {
	caBundleOne := []byte("ca-bundle-for-mwc")
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()

	mwc := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mwc-one",
		},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name: "webhook-under-test",
			},
		},
	}
	mwcCreated, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, mwc, metav1.CreateOptions{})
	require.NoError(t, err)
	err = UpdateWithCABundle(ctx, clientset, mwcCreated.Name, caBundleOne)
	require.NoError(t, err)
	mwcFetched, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, mwc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, caBundleOne, mwcFetched.Webhooks[0].ClientConfig.CABundle)
}

func TestUpdateWithCABundle_patchesExistingConfigurationWhenMutatingAndValidatingExist(t *testing.T) {
	caBundleOne := []byte("ca-bundle-for-mwc")
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()

	vwc := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mwc-one",
		},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name: "webhook-under-test",
			},
		},
	}

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
	_, err = clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, vwc, metav1.CreateOptions{})
	require.NoError(t, err)
	err = UpdateWithCABundle(ctx, clientset, mwcCreated.Name, caBundleOne)
	require.NoError(t, err)
	vwcFetched, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, vwc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, caBundleOne, vwcFetched.Webhooks[0].ClientConfig.CABundle)

	mwcFetched, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, mwc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, caBundleOne, mwcFetched.Webhooks[0].ClientConfig.CABundle)
}
