// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package snapshotagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSnapshotAgent_K8sSecret installs snapshot agent config with an embedded token as a k8s secret.
// It then installs Consul with k8s as a secrets backend and verifies that snapshot files
// are generated.
// Currently, the token needs to be embedded in the snapshot agent config due to a Consul
// bug that does not recognize the token for snapshot command being configured via
// a command line arg or an environment variable.
func TestSnapshotAgent_K8sSecret(t *testing.T) {
	cfg := suite.Config()
	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set and snapshot agent is already tested with regular tproxy")
	}

	cases := map[string]struct {
		secure bool
	}{
		"non-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			kubectlOptions := ctx.KubectlOptions(t)
			ns := kubectlOptions.Namespace
			releaseName := helpers.RandomName()

			saSecretName := fmt.Sprintf("%s-snapshot-agent-config", releaseName)
			saSecretKey := "config"

			// Create cluster
			helmValues := map[string]string{
				"global.tls.enabled":                           strconv.FormatBool(c.secure),
				"global.gossipEncryption.autoGenerate":         strconv.FormatBool(c.secure),
				"global.acls.manageSystemACLs":                 strconv.FormatBool(c.secure),
				"server.snapshotAgent.enabled":                 "true",
				"server.snapshotAgent.configSecret.secretName": saSecretName,
				"server.snapshotAgent.configSecret.secretKey":  saSecretKey,
				"connectInject.enabled":                        "false",
			}

			// Get new cluster
			consulCluster := consul.NewHelmCluster(t, helmValues, suite.Environment().DefaultContext(t), cfg, releaseName)
			client := environment.KubernetesClientFromOptions(t, kubectlOptions)

			// Add snapshot agent config secret
			logger.Log(t, "Storing snapshot agent config as a k8s secret")
			config := generateSnapshotAgentConfig(t)
			logger.Logf(t, "Snapshot agent config: %s", config)
			consul.CreateK8sSecret(t, client, cfg, ns, saSecretName, saSecretKey, config)

			// Create cluster
			consulCluster.Create(t)
			// ----------------------------------

			// Validate that consul snapshot agent is running correctly and is generating snapshot files
			logger.Log(t, "Confirming that Consul Snapshot Agent is generating snapshot files")
			// Create k8s client from kubectl options.

			podList, err := client.CoreV1().Pods(kubectlOptions.Namespace).List(context.Background(),
				metav1.ListOptions{LabelSelector: fmt.Sprintf("app=consul,component=server,release=%s", releaseName)})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1, "expected to find only 1 consul server instance")

			// We need to give some extra time for ACLs to finish bootstrapping and for servers to come up.
			timer := &retry.Timer{Timeout: 1 * time.Minute, Wait: 1 * time.Second}
			retry.RunWith(timer, t, func(r *retry.R) {
				// Loop through snapshot agents.  Only one will be the leader and have the snapshot files.
				pod := podList.Items[0]
				snapshotFileListOutput, err := k8s.RunKubectlAndGetOutputWithLoggerE(r, kubectlOptions, terratestLogger.Discard, "exec", pod.Name, "-c", "consul-snapshot-agent", "--", "ls", "/tmp")
				require.NoError(r, err)
				logger.Logf(r, "Snapshot: \n%s", snapshotFileListOutput)
				require.Contains(r, snapshotFileListOutput, ".snap", "Agent pod does not contain snapshot files")
			})
		})
	}
}

func generateSnapshotAgentConfig(t *testing.T) string {
	config := map[string]interface{}{
		"snapshot_agent": map[string]interface{}{
			"log": map[string]interface{}{
				"level":           "INFO",
				"enable_syslog":   false,
				"syslog_facility": "LOCAL0",
			},
			"snapshot": map[string]interface{}{
				"interval":           "5s",
				"retain":             30,
				"stale":              false,
				"service":            "consul-snapshot",
				"deregister_after":   "72h",
				"lock_key":           "consul-snapshot/lock",
				"max_failures":       3,
				"local_scratch_path": "",
			},
			"local_storage": map[string]interface{}{
				"path": "/tmp",
			},
		},
	}
	buf := bytes.NewBuffer(nil)
	err := json.NewEncoder(buf).Encode(config)
	require.NoError(t, err)
	jsonConfig, err := json.Marshal(&config)
	require.NoError(t, err)
	return string(jsonConfig)
}
