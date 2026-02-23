package helmvalues

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConsulFullName returns the full name for Consul resources based on Helm values.
// This mirrors the logic from the Helm template "consul.fullname".
func ConsulFullName(hv *HelmValues) string {
	if hv.FullNameOverride != "" {
		return truncateAndTrim(hv.FullNameOverride, 63)
	}
	if hv.Global.Name != "" {
		return truncateAndTrim(hv.Global.Name, 63)
	}
	name := hv.NameOverride
	if name == "" {
		name = "consul"
	}
	return truncateAndTrim(fmt.Sprintf("%s-%s", hv.Release.Name, name), 63)
}

// truncateAndTrim truncates a string to maxLen and trims trailing hyphens.
func truncateAndTrim(s string, maxLen int) string {
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return trimSuffix(s, "-")
}

// trimSuffix removes trailing occurrences of suffix from s.
func trimSuffix(s, suffix string) string {
	for len(s) > 0 && len(suffix) > 0 && s[len(s)-1:] == suffix {
		s = s[:len(s)-1]
	}
	return s
}

// GetHelmValues retrieves the Helm values from the ConfigMap.
func GetHelmValues(ctx context.Context, k8sClient client.Client, releaseName, namespace string) (*HelmValues, error) {
	cm := &corev1.ConfigMap{}
	configMapName := fmt.Sprintf("%s-consul-helm-values", releaseName)

	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      configMapName,
		Namespace: namespace,
	}, cm)
	if err != nil {
		return nil, fmt.Errorf("failed to get helm values configmap: %w", err)
	}

	data, ok := cm.Data["values.json"]
	if !ok {
		return nil, fmt.Errorf("values.json not found in configmap %s", configMapName)
	}

	var values HelmValues
	if err := json.Unmarshal([]byte(data), &values); err != nil {
		return nil, fmt.Errorf("failed to unmarshal helm values: %w", err)
	}

	return &values, nil
}

// ConsulName mirrors Helm helper `consul.name` (helpers.tpl ~211-214):
//
//	default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-"
func ConsulName(hv *HelmValues) string {
	name := "consul"
	if hv != nil {
		if s := strings.TrimSpace(hv.NameOverride); s != "" {
			name = s
		}
	}
	return truncDNSLabel(name)
}

func truncDNSLabel(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 63 {
		s = s[:63]
	}
	return strings.TrimSuffix(s, "-")
}

// ConsulChart mirrors Helm helper `consul.chart` (helpers.tpl ~203-206):
//
//	printf "%s-helm" .Chart.Name | replace "+" "_" | trunc 63 | trimSuffix "-"
func ConsulChart() string {
	chartName := "consul"
	// If you later add Chart.Name to HelmValues, prefer it here.
	out := fmt.Sprintf("%s-helm", chartName)
	out = strings.ReplaceAll(out, "+", "_")
	return truncDNSLabel(out)
}
