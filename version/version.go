// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package version

import (
	_ "embed"
	"fmt"
	"strings"
)

var (
	// GitCommit is the git commit that was compiled.
	// This will be filled in by the compiler.
	GitCommit string
	// GitDescribe is a bit of a misnomer. It's really the product version we set during CI builds,
	// which will match the git tag of that release once it's promoted.
	// This will be filled in by the compiler.
	GitDescribe string

	// The next version number that will be released. This will be updated after every release.
	// Version must conform to the format expected by github.com/hashicorp/go-version
	// for tests to work.
	// A pre-release marker for the version can also be specified (e.g -dev). If this is omitted
	// then it means that it is a final release. Otherwise, this is a pre-release
	// such as "dev" (in development), "beta", "rc1", etc.
	//go:embed VERSION
	fullVersion string

	Version, versionPrerelease, _ = strings.Cut(strings.TrimSpace(fullVersion), "-")
)

// GetHumanVersion composes the parts of the version in a way that's suitable
// for displaying to humans.
func GetHumanVersion() string {
	version := Version
	if GitDescribe != "" {
		version = GitDescribe
	}
	version = fmt.Sprintf("v%s", version)

	release := versionPrerelease
	if GitDescribe == "" && release == "" {
		release = "dev"
	}

	if IsFIPS() {
		version += "+fips1402"
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
