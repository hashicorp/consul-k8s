// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import "strconv"

func ParseScrapePort(v string) (int, bool) {
	port, err := strconv.Atoi(v)
	if err != nil {
		// we only use the port if it's actually valid
		return 0, false
	}
	if port < 1024 || port > 65535 {
		return 0, false
	}
	return port, true
}

func GetScrapePath(v string) (string, bool) {
	return v, v != ""
}

func GetMetricsEnabled(v string) (bool, bool) {
	if v == "true" {
		return true, true
	}
	if v == "false" {
		return false, true
	}
	return false, false
}
