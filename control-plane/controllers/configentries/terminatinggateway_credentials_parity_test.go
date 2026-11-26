// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package configentries

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

var updateCredentialParityGolden = flag.Bool("update-credential-parity", false, "regenerate the terminating-gateway credential-injection parity golden file")

// credentialParityGoldenPath is shared with the Helm chart test
// "credentialInjection controller and Helm render the same pod" in
// charts/consul/test/unit/terminating-gateways-deployment.bats, which renders
// the chart with the same inputs and compares against this file. Changing the
// credential-injection projection on either side without the other fails one
// of the two tests.
var credentialParityGoldenPath = filepath.Join("testdata", "terminating-gateway-credential-injection-parity.golden.json")

// TestTerminatingGatewayCredentialPodHelmParity guards against drift between the
// controller-built and Helm-rendered credential-injection pod. The inputs must
// match the --set values used by the corresponding bats test.
func TestTerminatingGatewayCredentialPodHelmParity(t *testing.T) {
	ci := &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:                true,
		ProcessorImage:         "camp-auth-processor:parity",
		VaultAgentImage:        "hashicorp/vault:parity",
		ProcessorConfigMap:     "camp-proc",
		VaultAgentConfigMap:    "camp-agent",
		VaultAddress:           "https://vault:8200",
		VaultCAConfigMap:       "camp-ca",
		VaultNamespace:         "camp",
		TokenAudience:          "vault",
		TokenExpirationSeconds: ptr.To(int64(600)),
		DrainSeconds:           ptr.To(int64(45)),
	}
	podSpec := corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "terminating-gateway-init"}},
		Containers:     []corev1.Container{{Name: "terminating-gateway"}},
	}
	// ACLs enabled, Vault Agent Injector disabled.
	applyTerminatingGatewayCredentialInjection(&podSpec, ci, corev1.PullIfNotPresent, "info", true, false)
	got := normalizeCredentialPod(t, podSpec)

	if *updateCredentialParityGolden {
		b, err := json.MarshalIndent(got, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(credentialParityGoldenPath), 0o755))
		require.NoError(t, os.WriteFile(credentialParityGoldenPath, append(b, '\n'), 0o644))
	}

	raw, err := os.ReadFile(credentialParityGoldenPath)
	require.NoError(t, err, "run `go test -run TestTerminatingGatewayCredentialPodHelmParity -update-credential-parity` to create the golden file")
	var want map[string]any
	require.NoError(t, json.Unmarshal(raw, &want))
	require.Equal(t, want, got,
		"controller credential-injection pod drifted from %s; update the Helm chart to match, then regenerate with -update-credential-parity", credentialParityGoldenPath)
}

// normalizeCredentialPod keeps only the credential-injection projection and
// orders it deterministically. It must stay in sync with the jq filter in the
// bats parity test.
func normalizeCredentialPod(t *testing.T, podSpec corev1.PodSpec) map[string]any {
	t.Helper()
	b, err := json.Marshal(podSpec)
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, json.Unmarshal(b, &spec))

	sortByName := func(items []any) []any {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].(map[string]any)["name"].(string) < items[j].(map[string]any)["name"].(string)
		})
		return items
	}
	asList := func(v any) []any {
		if l, ok := v.([]any); ok {
			return l
		}
		return []any{}
	}

	containers := []any{}
	for _, c := range append(asList(spec["initContainers"]), asList(spec["containers"])...) {
		m := c.(map[string]any)
		if !strings.HasPrefix(m["name"].(string), "camp-") {
			continue
		}
		m["env"] = sortByName(asList(m["env"]))
		m["volumeMounts"] = sortByName(asList(m["volumeMounts"]))
		if _, ok := m["resources"]; !ok {
			m["resources"] = map[string]any{}
		}
		containers = append(containers, m)
	}

	volumes := []any{}
	for _, v := range asList(spec["volumes"]) {
		name := v.(map[string]any)["name"].(string)
		if strings.HasPrefix(name, "camp-") || name == campConsulAuthTokenVolume {
			volumes = append(volumes, v)
		}
	}

	out := map[string]any{
		"containers": containers,
		"volumes":    sortByName(volumes),
	}
	for _, k := range []string{"securityContext", "terminationGracePeriodSeconds", "automountServiceAccountToken"} {
		if v, ok := spec[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out
}
