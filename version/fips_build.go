// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build fips

package version

// The in-tree Go Cryptographic Module is selected at build time with GOFIPS140
// and activated at run time via //go:debug fips140=on (baked into the main
// packages). In FIPS mode the module constrains crypto/tls to FIPS-approved
// settings, so a separate crypto/tls/fipsonly import (boringcrypto-only) is not
// used here.
import (
	"crypto/fips140"
)

// IsFIPS returns true if consul-k8s is operating in FIPS-140-3 mode.
func IsFIPS() bool {
	return true
}

func GetFIPSInfo() string {
	// The in-tree Go Cryptographic Module reports its validated version via
	// crypto/fips140.Version() (e.g. "v1.0.0", CMVP Certificate #5247).
	return "FIPS 140-3 Enabled, crypto module " + fips140.Version()
}
