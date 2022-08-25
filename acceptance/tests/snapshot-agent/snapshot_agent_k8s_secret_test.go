package snapshotagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/go-uuid"
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
	ctx := suite.Environment().DefaultContext(t)
	kubectlOptions := ctx.KubectlOptions(t)
	ns := kubectlOptions.Namespace
	releaseName := helpers.RandomName()

	// Generate a bootstrap token
	bootstrapToken, err := uuid.GenerateUUID()
	require.NoError(t, err)

	bsSecretName := fmt.Sprintf("%s-acl-bootstrap-token", releaseName)
	bsSecretKey := "token"
	saSecretName := fmt.Sprintf("%s-snapshot-agent-config", releaseName)
	saSecretKey := "token"

	// Create cluster
	helmValues := map[string]string{
		"global.tls.enabled":                           "true",
		"global.gossipEncryption.autoGenerate":         "true",
		"global.acls.manageSystemACLs":                 "true",
		"global.acls.bootstrapToken.secretName":        bsSecretName,
		"global.acls.bootstrapToken.secretKey":         bsSecretKey,
		"client.snapshotAgent.enabled":                 "true",
		"client.snapshotAgent.configSecret.secretName": saSecretName,
		"client.snapshotAgent.configSecret.secretKey":  saSecretKey,
	}

	// Get new cluster
	consulCluster := consul.NewHelmCluster(t, helmValues, suite.Environment().DefaultContext(t), cfg, releaseName)
	client := environment.KubernetesClientFromOptions(t, kubectlOptions)

	// Add bootstrap token secret
	logger.Log(t, "Storing bootstrap token as a k8s secret")
	consul.CreateK8sSecret(t, client, cfg, ns, bsSecretName, bsSecretKey, bootstrapToken)

	// Add snapshot agent config secret
	logger.Log(t, "Storing snapshot agent config as a k8s secret")
	config := generateSnapshotAgentConfig(t, bootstrapToken)
	logger.Logf(t, "Snapshot agent config: %s", config)
	consul.CreateK8sSecret(t, client, cfg, ns, saSecretName, saSecretKey, config)

	// Create cluster
	consulCluster.Create(t)
	// ----------------------------------

	// Validate that consul snapshot agent is running correctly and is generating snapshot files
	logger.Log(t, "Confirming that Consul Snapshot Agent is generating snapshot files")
	// Create k8s client from kubectl options.

	podList, err := client.CoreV1().Pods(kubectlOptions.Namespace).List(context.Background(),
		metav1.ListOptions{LabelSelector: fmt.Sprintf("app=consul,component=client-snapshot-agent,release=%s", releaseName)})
	require.NoError(t, err)
	require.True(t, len(podList.Items) > 0)

	// Wait for 10seconds to allow snapsot to write.
	time.Sleep(10 * time.Second)

	// Loop through snapshot agents.  Only one will be the leader and have the snapshot files.
	hasSnapshots := false
	for _, pod := range podList.Items {
		snapshotFileListOutput, err := k8s.RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, terratestLogger.Discard, "exec", pod.Name, "-c", "consul-snapshot-agent", "--", "ls", "/")
		logger.Logf(t, "Snapshot: \n%s", snapshotFileListOutput)
		require.NoError(t, err)
		if strings.Contains(snapshotFileListOutput, ".snap") {
			logger.Logf(t, "Agent pod contains snapshot files")
			hasSnapshots = true
			break
		} else {
			logger.Logf(t, "Agent pod does not contain snapshot files")
		}
	}
	require.True(t, hasSnapshots, ".snap")
}

func generateSnapshotAgentConfig(t *testing.T, token string) string {
	config := map[string]interface{}{
		"snapshot_agent": map[string]interface{}{
			"token": token,
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
				"path": ".",
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
