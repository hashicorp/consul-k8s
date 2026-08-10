// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build fips

//go:debug fips140=on

package main

// FIPS builds run the in-tree Go Cryptographic Module in FIPS Mode. The
// //go:debug fips140=on directive above bakes GODEBUG=fips140=on into the
// binary so operators do not need to set an environment variable. The module
// runs its own pre-operational and conditional self-tests on init and aborts
// the process on failure (module-enforced fail-closed).
