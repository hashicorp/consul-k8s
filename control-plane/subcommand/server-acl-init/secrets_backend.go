// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

type SecretsBackendType string

type SecretsBackend interface {
	// BootstrapToken fetches the bootstrap token from the backend. If the
	// token is not found or empty, implementations should return an empty
	// string (not an error).
	BootstrapToken() (string, error)

	// WriteBootstrapToken writes the given bootstrap token to the backend.
	// Implementations of this method do not need to retry the write until
	// successful.
	WriteBootstrapToken(string) error

	// BootstrapTokenSecretName returns the name of the bootstrap token secret.
	BootstrapTokenSecretName() string
}
