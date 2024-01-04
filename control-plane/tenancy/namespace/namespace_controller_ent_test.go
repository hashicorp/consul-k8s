// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package namespace

import (
	"testing"
)

func TestReconcileCreateNamespace_ENT(t *testing.T) {
	t.Parallel()

	testCases := []createTestCase{
		{
			name:                       "destination consul namespace is ap1/ns1",
			kubeNamespace:              "kube-ns1",
			partition:                  "ap1",
			mirroringK8s:               false,
			consulDestinationNamespace: "ns1",
			expectedConsulNamespace:    "ns1",
		},
		{
			name:                    "mirrored consul namespace is ap1/ns1",
			kubeNamespace:           "ns1",
			partition:               "ap1",
			mirroringK8s:            true,
			expectedConsulNamespace: "ns1",
		},
		{
			name:                    "mirrored namespaces with prefix to ap1/k8s-ns1",
			kubeNamespace:           "ns1",
			partition:               "ap1",
			mirroringK8s:            true,
			mirroringK8sPrefix:      "k8s-",
			expectedConsulNamespace: "k8s-ns1",
		},
	}
	testReconcileCreateNamespace(t, testCases)
}

func TestReconcileDeleteNamespace_ENT(t *testing.T) {
	t.Parallel()

	testCases := []deleteTestCase{
		{
			name:                       "destination namespace with non-default is not cleaned up, non-default partition",
			kubeNamespace:              "ns1",
			partition:                  "ap1",
			consulDestinationNamespace: "ns1",
			mirroringK8s:               false,
			existingConsulNamespace:    "ns1",
			expectNamespaceExists:      "ns1",
		},
		{
			name:                    "mirrored namespaces, non-default partition",
			kubeNamespace:           "ns1",
			partition:               "ap1",
			mirroringK8s:            true,
			existingConsulNamespace: "ns1",
			expectNamespaceDeleted:  "ns1",
		},
		{
			name:                    "mirrored namespaces with prefix, non-default partition",
			kubeNamespace:           "ns1",
			partition:               "ap1",
			mirroringK8s:            true,
			mirroringK8sPrefix:      "k8s-",
			existingConsulNamespace: "k8s-ns1",
			expectNamespaceDeleted:  "k8s-ns1",
		},
	}
	testReconcileDeleteNamespace(t, testCases)
}
