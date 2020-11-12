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
		flagEntLicenseSecretName string
		flagEntLicenseSecretKey  string
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
			"enterprise license: error when only -enterprise-license-secret-name is provided",
			fields{
				flagEntLicenseSecretName: "secret",
			},
			true,
			"both of -enterprise-license-secret-name and -enterprise-license-secret-name flags must be provided; not just one",
		},
		{
			"enterprise license: error when only -enterprise-license-secret-key is provided",
			fields{
				flagEntLicenseSecretKey: "key",
			},
			true,
			"both of -enterprise-license-secret-name and -enterprise-license-secret-name flags must be provided; not just one",
		},
		{
			"enterprise license: no error when both -enterprise-license-secret-name and -enterprise-license-secret-key are provided",
			fields{
				flagEntLicenseSecretName: "secret",
				flagEntLicenseSecretKey:  "key",
			},
			false,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tf := &TestFlags{
				flagEnableMultiCluster:          tt.fields.flagEnableMultiCluster,
				flagSecondaryKubeconfig:         tt.fields.flagSecondaryKubeconfig,
				flagSecondaryKubecontext:        tt.fields.flagSecondaryKubecontext,
				flagEnterpriseLicenseSecretName: tt.fields.flagEntLicenseSecretName,
				flagEnterpriseLicenseSecretKey:  tt.fields.flagEntLicenseSecretKey,
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
