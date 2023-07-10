// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package version

import (
	"fmt"
	"strings"
)

var (
	// The git commit that was compiled. These will be filled in by the compiler.
	GitCommit   string
	GitDescribe string

	// The main version number that is being run at the moment.
	//
	// Version must conform to the format expected by
	// github.com/hashicorp/go-version for tests to work.
	Version = "1.3.0"

	// A pre-release marker for the version. If this is "" (empty string)
	// then it means that it is a final release. Otherwise, this is a pre-release
	// such as "dev" (in development), "beta", "rc1", etc.
	VersionPrerelease = "dev"
)

// GetHumanVersion composes the parts of the version in a way that's suitable
// for displaying to humans.
func GetHumanVersion() string {
	version := Version
	if GitDescribe != "" {
		version = GitDescribe
	}
	version = fmt.Sprintf("v%s", version)

	release := VersionPrerelease
	if GitDescribe == "" && release == "" {
		release = "dev"
	}

	if IsFIPS() {
		version += ".fips1402"
	}

	if release != "" {
		if !strings.Contains(version, "-"+release) {
			// if we tagged a prerelease version then the release is in the version already
			version += fmt.Sprintf("-%s", release)
		}
		if GitCommit != "" {
			version += fmt.Sprintf(" (%s)", GitCommit)
		}
	}

	// Strip off any single quotes added by the git information.
	return strings.Replace(version, "'", "", -1)
}
