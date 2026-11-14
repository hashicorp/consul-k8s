// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build windows

// Package nftlock serializes access to the nft bundle. The CNI plugin and installer only run on Linux; this
// no-op implementation lets the control plane build on Windows.
package nftlock

import "time"

// FileName is the name of the lock file in the CNI bin directory.
const FileName = ".consul-nft.lock"

func Shared(string, time.Duration) (func(), error) { return func() {}, nil }

func Exclusive(string, time.Duration) (func(), error) { return func() {}, nil }
