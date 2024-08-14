// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookconfiguration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// UpdateWithCABundle iterates over every webhook on the specified webhook configuration and updates
// their caBundle with the the specified CA.
func UpdateWithCABundle(ctx context.Context, clientset kubernetes.Interface, webhookConfigName string, caCert []byte) error {
	if len(caCert) == 0 {
		return errors.New("no CA certificate in the bundle")
	}

	mutatingWebhookCfg, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigName, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if !k8serrors.IsNotFound(err) {
		err = updateMutatingWebhooksWithCABundle(ctx, clientset, mutatingWebhookCfg, caCert)
		if err != nil {
			return err
		}
	}

	validatingWebhookCfg, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, webhookConfigName, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if k8serrors.IsNotFound(err) {
		return nil
	}

	return updateValidatingWebhooksWithCABundle(ctx, clientset, validatingWebhookCfg, caCert)
}

func updateMutatingWebhooksWithCABundle(ctx context.Context, clientset kubernetes.Interface, webhookCfg *admissionv1.MutatingWebhookConfiguration, caCert []byte) error {
	value := base64.StdEncoding.EncodeToString(caCert)
	type patch struct {
		Op    string `json:"op,omitempty"`
		Path  string `json:"path,omitempty"`
		Value string `json:"value,omitempty"`
	}

	var patches []patch
	for i := range webhookCfg.Webhooks {
		patches = append(patches, patch{
			Op:    "add",
			Path:  fmt.Sprintf("/webhooks/%d/clientConfig/caBundle", i),
			Value: value,
		})
	}
	patchesJSON, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	if _, err = clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Patch(ctx, webhookCfg.Name, types.JSONPatchType, patchesJSON, metav1.PatchOptions{}); err != nil {
		return err
	}

	return nil
}

func updateValidatingWebhooksWithCABundle(ctx context.Context, clientset kubernetes.Interface, webhookCfg *admissionv1.ValidatingWebhookConfiguration, caCert []byte) error {
	value := base64.StdEncoding.EncodeToString(caCert)
	type patch struct {
		Op    string `json:"op,omitempty"`
		Path  string `json:"path,omitempty"`
		Value string `json:"value,omitempty"`
	}

	var patches []patch
	for i := range webhookCfg.Webhooks {
		patches = append(patches, patch{
			Op:    "add",
			Path:  fmt.Sprintf("/webhooks/%d/clientConfig/caBundle", i),
			Value: value,
		})
	}
	patchesJSON, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	if _, err = clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Patch(ctx, webhookCfg.Name, types.JSONPatchType, patchesJSON, metav1.PatchOptions{}); err != nil {
		return err
	}

	return nil
}
