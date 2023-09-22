// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2alpha1

import (
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

func meshConfigMeta() map[string]string {
	return map[string]string{
		common.SourceKey: common.SourceValue,
	}
}
