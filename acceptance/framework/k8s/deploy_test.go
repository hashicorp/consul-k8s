// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenShiftStaticWorkloadFixture(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    string
		wantCNI string
	}{
		{fixture: "../fixtures/cases/static-server-inject", want: "../fixtures/cases/static-server-openshift", wantCNI: "../fixtures/cases/static-server-openshift-cni"},
		{fixture: "../fixtures/cases/static-client-inject", want: "../fixtures/cases/static-client-openshift-inject", wantCNI: "../fixtures/cases/static-client-openshift-inject-cni"},
		{fixture: "../fixtures/cases/static-client-tproxy", want: "../fixtures/cases/static-client-openshift-tproxy", wantCNI: "../fixtures/cases/static-client-openshift-tproxy-cni"},
		{fixture: "../fixtures/bases/static-server-tcp", want: "../fixtures/bases/static-server-tcp", wantCNI: "../fixtures/cases/static-server-tcp-openshift-cni"},
		{fixture: "../fixtures/cases/static-client-namespaces", want: "../fixtures/cases/static-client-namespaces", wantCNI: "../fixtures/cases/static-client-namespaces"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			require.Equal(t, tc.want, openShiftStaticWorkloadFixture(tc.fixture, false))
			require.Equal(t, tc.wantCNI, openShiftStaticWorkloadFixture(tc.fixture, true))
			require.DirExists(t, "../../tests/"+tc.want[len("../"):])
			require.DirExists(t, "../../tests/"+tc.wantCNI[len("../"):])
		})
	}
}
