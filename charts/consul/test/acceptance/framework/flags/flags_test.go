package flags

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlags_validate(t *testing.T) {
	type fields struct {
		flagEnableMultiCluster   bool
		flagSecondaryKubeconfig  string
		flagSecondaryKubecontext string

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
			fields{
				flagEnableMultiCluster:   false,
				flagSecondaryKubeconfig:  "",
				flagSecondaryKubecontext: "",
			},
			false,
			"",
		},
		{
			"enable multi cluster: no error when multi cluster is disabled",
			fields{
				flagEnableMultiCluster:   false,
				flagSecondaryKubeconfig:  "",
				flagSecondaryKubecontext: "",
			},
			false,
			"",
		},
		{
			"enable multi cluster: errors when both secondary kubeconfig and kubecontext are empty",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "",
				flagSecondaryKubecontext: "",
			},
			true,
			"at least one of -secondary-kubecontext or -secondary-kubeconfig flags must be provided if -enable-multi-cluster is set",
		},
		{
			"enable multi cluster: no error when secondary kubeconfig but not kubecontext is provided",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "foo",
				flagSecondaryKubecontext: "",
			},
			false,
			"",
		},
		{
			"enable multi cluster: no error when secondary kubecontext but not kubeconfig is provided",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "",
				flagSecondaryKubecontext: "foo",
			},
			false,
			"",
		},
		{
			"enable multi cluster: no error when both secondary kubecontext and kubeconfig are provided",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "foo",
				flagSecondaryKubecontext: "bar",
			},
			false,
			"",
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
				flagEnableMultiCluster:   tt.fields.flagEnableMultiCluster,
				flagSecondaryKubeconfig:  tt.fields.flagSecondaryKubeconfig,
				flagSecondaryKubecontext: tt.fields.flagSecondaryKubecontext,
				flagEnableEnterprise:     tt.fields.flagEnableEnt,
				flagEnterpriseLicense:    tt.fields.flagEntLicense,
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
