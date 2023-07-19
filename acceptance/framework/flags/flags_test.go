// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package flags

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlags_validate(t *testing.T) {
	type fields struct {
		flagEnableMultiCluster bool
		flagKubeConfigs        listFlag
		flagKubeContexts       listFlag
		flagNamespaces         listFlag

		flagEnableEnt  bool
		flagEntLicense string
	}
	tests := []struct {
		name       string
		fields     fields
		wantErr    bool
		errMessage string
	}{
		{
			"no error by default",
			fields{},
			false,
			"",
		},
		{
			"enable multi cluster: no error when multi cluster is disabled",
			fields{
				flagEnableMultiCluster: false,
				flagKubeConfigs:        listFlag{},
				flagKubeContexts:       listFlag{},
			},
			false,
			"",
		},
		{
			"enable multi cluster: errors when both secondary kubeconfig and kubecontext are empty",
			fields{
				flagEnableMultiCluster: true,
				flagKubeConfigs:        listFlag{},
				flagKubeContexts:       listFlag{},
			},
			true,
			"at least two contexts must be included in -kube-contexts or -kubeconfigs if -enable-multi-cluster is set",
		},
		{
			"enable multi cluster: no error when secondary kubeconfig but not kubecontext is provided",
			fields{
				flagEnableMultiCluster: true,
				flagKubeConfigs:        listFlag{"foo", "bar"},
				flagKubeContexts:       listFlag{},
			},
			false,
			"",
		},
		{
			"enable multi cluster: no error when secondary kubecontext but not kubeconfig is provided",
			fields{
				flagEnableMultiCluster: true,
				flagKubeConfigs:        listFlag{},
				flagKubeContexts:       listFlag{"foo", "bar"},
			},
			false,
			"",
		},
		{
			"enable multi cluster: no error when both secondary kubecontext and kubeconfig are provided",
			fields{
				flagEnableMultiCluster: true,
				flagKubeConfigs:        listFlag{"foo", "bar"},
				flagKubeContexts:       listFlag{"foo", "bar"},
			},
			false,
			"",
		},
		{
			"enable multi cluster: no error when all of secondary kubecontext, kubeconfigs and namespaces are provided",
			fields{
				flagEnableMultiCluster: true,
				flagKubeConfigs:        listFlag{"foo", "bar"},
				flagKubeContexts:       listFlag{"foo", "bar"},
				flagNamespaces:         listFlag{"foo", "bar"},
			},
			false,
			"",
		},
		{
			"enable multi cluster: error when the list of kubeconfigs and kubecontexts do not match",
			fields{
				flagEnableMultiCluster: true,
				flagKubeConfigs:        listFlag{"foo", "bar"},
				flagKubeContexts:       listFlag{"foo"},
			},
			true,
			"-kube-contexts and -kubeconfigs are both set but are not of equal length",
		},
		{
			"enable multi cluster: error when the list of kubeconfigs and namespaces do not match",
			fields{
				flagEnableMultiCluster: true,
				flagKubeConfigs:        listFlag{"foo", "bar"},
				flagNamespaces:         listFlag{"foo"},
			},
			true,
			"-kube-namespaces and -kubeconfigs are both set but are not of equal length",
		},
		{
			"enable multi cluster: error when the list of kubecontexts and namespaces do not match",
			fields{
				flagEnableMultiCluster: true,
				flagKubeContexts:       listFlag{"foo", "bar"},
				flagNamespaces:         listFlag{"foo"},
			},
			true,
			"-kube-contexts and -kube-namespaces are both set but are not of equal length",
		},
		{
			"enterprise license: error when only -enable-enterprise is true but env CONSUL_ENT_LICENSE is not provided",
			fields{
				flagEnableEnt: true,
			},
			true,
			"-enable-enterprise provided without setting env var CONSUL_ENT_LICENSE with consul license",
		},
		{
			"enterprise license: no error when both -enable-enterprise and env CONSUL_ENT_LICENSE are provided",
			fields{
				flagEnableEnt:  true,
				flagEntLicense: "license",
			},
			false,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tf := &TestFlags{
				flagEnableMultiCluster: tt.fields.flagEnableMultiCluster,
				flagKubeconfigs:        tt.fields.flagKubeConfigs,
				flagKubecontexts:       tt.fields.flagKubeContexts,
				flagKubeNamespaces:     tt.fields.flagNamespaces,
				flagEnableEnterprise:   tt.fields.flagEnableEnt,
				flagEnterpriseLicense:  tt.fields.flagEntLicense,
			}
			err := tf.Validate()
			if tt.wantErr {
				require.EqualError(t, err, tt.errMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
