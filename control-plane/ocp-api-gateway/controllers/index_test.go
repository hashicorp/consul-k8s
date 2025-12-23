// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import "sigs.k8s.io/controller-runtime/pkg/client/fake"

func registerFieldIndexersForTest(clientBuilder *fake.ClientBuilder) *fake.ClientBuilder {
	for _, index := range indexes {
		clientBuilder = clientBuilder.WithIndex(index.target, index.name, index.indexerFunc)
	}
	return clientBuilder
}
