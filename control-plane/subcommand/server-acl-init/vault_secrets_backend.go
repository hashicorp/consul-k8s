// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"fmt"

	"github.com/hashicorp/vault/api"
)

const SecretsBackendTypeVault SecretsBackendType = "vault"

type VaultSecretsBackend struct {
	vaultClient *api.Client
	secretName  string
	secretKey   string
}

var _ SecretsBackend = (*VaultSecretsBackend)(nil)

// BootstrapToken returns the bootstrap token stored in Vault.
// If not found this returns an empty string (not an error).
func (b *VaultSecretsBackend) BootstrapToken() (string, error) {
	secret, err := b.vaultClient.Logical().Read(b.secretName)
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Data == nil {
		// secret not found or empty.
		return "", nil
	}
	// Grab secret.Data["data"][secretKey].
	dataRaw, found := secret.Data["data"]
	if !found {
		return "", nil
	}
	data, ok := dataRaw.(map[string]interface{})
	if !ok {
		return "", nil
	}
	tokRaw, found := data[b.secretKey]
	if !found {
		return "", nil
	}
	if tok, ok := tokRaw.(string); ok {
		return tok, nil
	}
	return "", fmt.Errorf("Unexpected data. To resolve this, "+
		"`vault kv put <path> %[1]s=<bootstrap-token>` if Consul is already ACL bootstrapped. "+
		"If not ACL bootstrapped, `vault kv put <path> %[1]s=\"\"`", b.secretKey, b.secretKey)
}

// BootstrapTokenSecretName returns the name of the bootstrap token secret.
func (b *VaultSecretsBackend) BootstrapTokenSecretName() string {
	return b.secretName
}

// WriteBootstrapToken writes the bootstrap token to Vault.
func (b *VaultSecretsBackend) WriteBootstrapToken(bootstrapToken string) error {
	_, err := b.vaultClient.Logical().Write(b.secretName,
		map[string]interface{}{
			"data": map[string]interface{}{
				b.secretKey: bootstrapToken,
			},
		},
	)
	return err
}
