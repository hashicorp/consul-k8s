package mutatingwebhookconfiguration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

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
	value := base64.StdEncoding.EncodeToString(caCert)
	webhookCfg, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigName, metav1.GetOptions{})

	if err != nil {
		return err
	}
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
	patchesJson, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	if _, err = clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Patch(ctx, webhookConfigName, types.JSONPatchType, patchesJson, metav1.PatchOptions{}); err != nil {
		return err
	}

	return nil
}
