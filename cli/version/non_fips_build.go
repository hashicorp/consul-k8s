// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build !fips

package version

// IsFIPS returns true if consul-k8s is operating in FIPS-140-2 mode.
func IsFIPS() bool {
	return false
}

func GetFIPSInfo() string {
	return ""
}
