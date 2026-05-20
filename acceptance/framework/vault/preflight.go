// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	vapi "github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// preflightRoleName is the name of the temporary Vault role created by
// WaitForAuthMethodReady to probe end-to-end login. It is cleaned up
// before the helper returns.
const preflightRoleName = "preflight-login"

// WaitForAuthMethodReady verifies that Vault's Kubernetes auth method at
// authPath is fully usable from the cluster reachable via k8sClient. It
// surfaces — in ~3 minutes, deterministically, before any helm install — the
// failure modes that otherwise hide for 45 minutes inside vault-agent's
// retry-with-exponential-backoff loop in a consul-k8s pre-install Job
// (partition-init, server-acl-init, etc.) and only show up as opaque
// "helm pre-install timed out".
//
// What it catches:
//
//  1. auth/<path>/config not yet persisted (Vault HA replication lag /
//     auth-method just enabled). Polls until kubernetes_host is set.
//  2. token_reviewer_jwt configured with an empty value (the K8s ≥ 1.24
//     SA-token race in ConfigureAuthMethod). The login probe below cannot
//     succeed if this is empty, so it transitively catches it.
//  3. Vault server → secondary K8s API TokenReview unreachable (private EKS
//     endpoint, SG/NACL misconfig, NAT egress IP not allowlisted). The
//     login probe issues a real TokenReview through Vault, so this is the
//     direct, end-to-end test.
//
// authSAName / authSANamespace must reference the ServiceAccount Vault uses
// as its TokenReviewer (the one passed to ConfigureAuthMethod). The helper
// creates a temporary Vault role bound to that SA, requests a short-lived
// JWT for it via the TokenRequest API, attempts a login, and tears the role
// down on success or failure.
func WaitForAuthMethodReady(t *testing.T, vaultClient *vapi.Client, k8sClient kubernetes.Interface, authPath, authSAName, authSANamespace string) {
	t.Helper()

	// (1) auth/<path>/config readable + kubernetes_host populated.
	logger.Logf(t, "[vault-preflight] waiting for auth/%s/config to be ready", authPath)
	configPath := fmt.Sprintf("auth/%s/config", authPath)
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 60}, t, func(r *retry.R) {
		secret, err := vaultClient.Logical().Read(configPath)
		if err != nil {
			r.Errorf("read %s: %v", configPath, err)
			return
		}
		if secret == nil || secret.Data == nil {
			r.Errorf("%s returned nil data", configPath)
			return
		}
		host, _ := secret.Data["kubernetes_host"].(string)
		if host == "" {
			r.Errorf("%s has empty kubernetes_host", configPath)
			return
		}
	})

	// (2) Create a temp role bound to the auth-method SA. We use a default
	// (no-op) policy by leaving token_policies empty; a successful login is
	// all we need for the probe.
	rolePath := fmt.Sprintf("auth/%s/role/%s", authPath, preflightRoleName)
	logger.Logf(t, "[vault-preflight] creating temporary role %s", rolePath)
	_, err := vaultClient.Logical().Write(rolePath, map[string]interface{}{
		"bound_service_account_names":      authSAName,
		"bound_service_account_namespaces": authSANamespace,
		"token_ttl":                        "60s",
		"token_max_ttl":                    "60s",
	})
	require.NoError(t, err, "creating preflight Vault role %s", rolePath)
	defer func() {
		if _, delErr := vaultClient.Logical().Delete(rolePath); delErr != nil {
			logger.Logf(t, "[vault-preflight] warning: failed to delete temporary role %s: %v", rolePath, delErr)
		}
	}()

	// (3) Mint a short-lived JWT for the auth-method SA via TokenRequest.
	logger.Logf(t, "[vault-preflight] requesting TokenRequest JWT for %s/%s", authSANamespace, authSAName)
	expirationSeconds := int64(600)
	tr, err := k8sClient.CoreV1().ServiceAccounts(authSANamespace).CreateToken(
		context.Background(),
		authSAName,
		&authv1.TokenRequest{
			Spec: authv1.TokenRequestSpec{
				ExpirationSeconds: &expirationSeconds,
			},
		},
		metav1.CreateOptions{},
	)
	require.NoError(t, err, "requesting TokenRequest JWT for SA %s/%s", authSANamespace, authSAName)
	require.NotEmpty(t, tr.Status.Token, "TokenRequest returned empty token")

	// (4) Probe auth/<path>/login until it succeeds or we hit the 3m budget.
	// 3m is well under helm's 15m hook timeout, so if this fails we surface
	// it before any install starts wasting time.
	loginPath := fmt.Sprintf("auth/%s/login", authPath)
	logger.Logf(t, "[vault-preflight] probing %s with role %s (budget 3m)", loginPath, preflightRoleName)
	start := time.Now()
	var lastErr error
	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 36}, t, func(r *retry.R) {
		resp, loginErr := vaultClient.Logical().Write(loginPath, map[string]interface{}{
			"role": preflightRoleName,
			"jwt":  tr.Status.Token,
		})
		if loginErr != nil {
			lastErr = loginErr
			r.Errorf("[vault-preflight] login %s still failing: %v", loginPath, loginErr)
			return
		}
		if resp == nil || resp.Auth == nil || resp.Auth.ClientToken == "" {
			lastErr = fmt.Errorf("empty auth response")
			r.Errorf("[vault-preflight] login %s returned empty auth", loginPath)
			return
		}
		// Revoke the just-issued token immediately so we don't leave it
		// dangling in Vault's token store.
		_ = vaultClient.Auth().Token().RevokeAccessor(resp.Auth.Accessor)
	})

	if lastErr != nil && strings.Contains(lastErr.Error(), "permission denied") {
		logger.Logf(t, "[vault-preflight] FINAL: %s returned permission denied for the full 3m budget; this usually means Vault's TokenReview against the kubernetes API is failing (private endpoint, SG, or empty token_reviewer_jwt)", loginPath)
	}
	logger.Logf(t, "[vault-preflight] %s reachable and accepting logins after %s", loginPath, time.Since(start))
}
