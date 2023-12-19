// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

func (b *meshGatewayBuilder) Annotations() map[string]string {

	var (
		annotationNamesToCopy = []string{}
		annotationNamesToAdd  = map[string]string{}
	)

	if b.gcc != nil {
		annotationNamesToCopy = b.gcc.Spec.Service.Annotations.InheritFromGateway
		annotationNamesToAdd = b.gcc.Spec.Service.Annotations.Set
	}

	out := map[string]string{}

	for _, v := range annotationNamesToCopy {
		out[v] = b.gateway.Annotations[v]
	}

	for k, v := range annotationNamesToAdd {
		out[k] = v
	}

	return out
}
