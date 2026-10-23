// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const openShiftDNSServerName = "consul-acceptance"

type openShiftDNSState struct {
	Metadata struct {
		ResourceVersion string `json:"resourceVersion"`
	} `json:"metadata"`
	Spec struct {
		Servers json.RawMessage `json:"servers"`
	} `json:"spec"`
}

func ConfigureOpenShiftDNS(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig, upstream string) {
	t.Helper()
	options := ctx.KubectlOptions(t)
	readState := func() openShiftDNSState {
		output, err := RunKubectlAndGetOutputE(t, options, "get", "dns.operator.openshift.io", "default", "-o", "json")
		require.NoError(t, err)
		var state openShiftDNSState
		require.NoError(t, json.Unmarshal([]byte(output), &state))
		if len(state.Spec.Servers) == 0 {
			state.Spec.Servers = json.RawMessage("null")
		}
		return state
	}
	state := readState()
	originalServers := state.Spec.Servers
	var servers []json.RawMessage
	require.NoError(t, json.Unmarshal(originalServers, &servers))
	for _, server := range servers {
		var existing struct {
			Name  string   `json:"name"`
			Zones []string `json:"zones"`
		}
		require.NoError(t, json.Unmarshal(server, &existing))
		require.False(t, slices.Contains(existing.Zones, "consul"), "existing Consul DNS forwarding conflicts with the acceptance test")
		require.NotEqual(t, openShiftDNSServerName, existing.Name, "the acceptance DNS forwarder already exists")
	}
	backup := struct {
		Context   string          `json:"context"`
		Namespace string          `json:"namespace"`
		Servers   json.RawMessage `json:"servers"`
		Upstream  string          `json:"upstream"`
	}{
		Context:   environment.KubernetesContextFromOptions(t, options),
		Namespace: options.Namespace,
		Servers:   originalServers,
		Upstream:  upstream,
	}
	backupJSON, err := json.Marshal(backup)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cfg.DebugDirectory, 0755))
	backupPath := filepath.Join(cfg.DebugDirectory, "ocp-dns-backup.json")
	backupFile, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	require.NoError(t, err, "restore the previous OpenShift DNS backup with cleanup-ocp-tests.sh before testing")
	_, err = backupFile.Write(backupJSON)
	require.NoError(t, err)
	require.NoError(t, backupFile.Close())

	patchServers := func(servers json.RawMessage, resourceVersion string) {
		patch, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]string{"resourceVersion": resourceVersion},
			"spec":     map[string]interface{}{"servers": servers},
		})
		require.NoError(t, err)
		output, err := RunKubectlAndGetOutputE(t, options, "patch", "dns.operator.openshift.io", "default", "--type=merge", "--patch", string(patch))
		require.NoError(t, err, output)
	}
	// Shared cluster DNS must be restored even when failed workloads are retained.
	t.Cleanup(func() {
		current := readState()
		restored, err := restoredOpenShiftDNSServers(current.Spec.Servers, originalServers, upstream)
		require.NoError(t, err)
		patchServers(restored, current.Metadata.ResourceVersion)
		require.NoError(t, os.Remove(backupPath))
	})
	server, err := json.Marshal(map[string]interface{}{
		"name":  openShiftDNSServerName,
		"zones": []string{"consul"},
		"forwardPlugin": map[string]interface{}{
			"policy":    "Sequential",
			"upstreams": []string{upstream},
		},
	})
	require.NoError(t, err)
	servers = append(servers, server)
	serversJSON, err := json.Marshal(servers)
	require.NoError(t, err)
	patchServers(serversJSON, state.Metadata.ResourceVersion)
	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		configMap, err := ctx.KubernetesClient(r).CoreV1().ConfigMaps("openshift-dns").Get(context.Background(), "dns-default", metav1.GetOptions{})
		require.NoError(r, err)
		require.Contains(r, configMap.Data["Corefile"], upstream)
	})
}

func restoredOpenShiftDNSServers(current, original json.RawMessage, upstream string) (json.RawMessage, error) {
	var servers []json.RawMessage
	if err := json.Unmarshal(current, &servers); err != nil {
		return nil, err
	}
	result := make([]json.RawMessage, 0, len(servers))
	for _, server := range servers {
		var entry struct {
			Name          string `json:"name"`
			ForwardPlugin struct {
				Upstreams []string `json:"upstreams"`
			} `json:"forwardPlugin"`
		}
		if err := json.Unmarshal(server, &entry); err != nil {
			return nil, err
		}
		if entry.Name != openShiftDNSServerName {
			result = append(result, server)
			continue
		}
		if !slices.Equal(entry.ForwardPlugin.Upstreams, []string{upstream}) {
			return nil, fmt.Errorf("the acceptance DNS forwarder changed; preserving the backup rather than overwriting another update")
		}
	}
	if len(result) == 0 && string(original) == "null" {
		return json.RawMessage("null"), nil
	}
	return json.Marshal(result)
}
