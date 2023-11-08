// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookv2

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

type ReadinessCheck struct {
	CertDir string
}

func (r ReadinessCheck) Ready(_ *http.Request) error {
	certFile, err := os.ReadFile(filepath.Join(r.CertDir, "tls.crt"))
	if err != nil {
		return err
	}
	keyFile, err := os.ReadFile(filepath.Join(r.CertDir, "tls.key"))
	if err != nil {
		return err
	}
	if len(certFile) == 0 || len(keyFile) == 0 {
		return errors.New("certificate files have not been loaded")
	}
	return nil
}
