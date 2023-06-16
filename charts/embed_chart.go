// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package charts

import "embed"

// ConsulHelmChart embeds the Consul Helm Chart files into an exported variable from this package. Changes to the chart
// files referenced below will be reflected in the embedded templates in the CLI at CLI compile time.
//
// This is currently only meant to be used by the consul-k8s CLI within this repository. Importing this package from the
// CLI allows us to embed the templates at compilation time. Since this is in a monorepo, we can directly reference this
// charts module as relative to the CLI module (with a replace directive), which allows us to not need to bump the
// charts module dependency manually or as part of a Makefile.
//
// The embed directive does not include files with underscores unless explicitly listed, which is why _helpers.tpl is
// explicitly embedded.

//go:embed consul/Chart.yaml consul/values.yaml consul/templates consul/templates/_helpers.tpl
var ConsulHelmChart embed.FS

//go:embed demo/Chart.yaml demo/values.yaml demo/templates
var DemoHelmChart embed.FS
