// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package load

import (
	"fmt"
	"strings"
)

// GetDomainSuffix gets the suffix.
func GetDomainSuffix(env string) (string, error) {
	suffix, ok := map[string]string{
		"dev-remote": "hcp.dev",
		"dev":        "hcp.dev",
		"int":        "hcp.to",
		"prod":       "hashicorp.cloud",
	}[strings.ToLower(env)]

	if !ok {
		return "", fmt.Errorf("unrecognized env: %s", env)
	}
	return suffix, nil
}

// GetAPIAddr returns the address of HCP given the passed environment.
func GetAPIAddr(env string) (string, error) {
	suffix, ok := map[string]string{
		"local":      "http://127.0.0.1:28081",
		"dev-remote": "https://api.hcp.dev",
		"dev":        "https://api.hcp.dev",
		"int":        "https://api.hcp.to",
		"prod":       "https://api.hashicorp.cloud",
	}[strings.ToLower(env)]

	if !ok {
		return "", fmt.Errorf("unrecognized env: %s", env)
	}
	return suffix, nil
}

// GetAuthIDP returns the authidp for the env.
func GetAuthIDP(env string) (string, error) {
	suffix, err := GetDomainSuffix(env)
	if err != nil {
		return "", fmt.Errorf("unrecognized env: %w", err)
	}

	return fmt.Sprintf("https://auth.idp.%s", suffix), nil
}

// GetScadaAddr returns the scadara for the env.
func GetScadaAddr(env string) (string, error) {
	suffix, err := GetDomainSuffix(env)
	if err != nil {
		return "", fmt.Errorf("unrecognized env: %w", err)
	}

	return fmt.Sprintf("https://scada.internal.%s:7224", suffix), nil
}

// GetScadaAddr returns the scadara for the env.
func GetScadaAddrWithoutProtocol(env string) (string, error) {
	suffix, err := GetDomainSuffix(env)
	if err != nil {
		return "", fmt.Errorf("unrecognized env: %w", err)
	}

	return fmt.Sprintf("scada.internal.%s:7224", suffix), nil
}
