package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlags_validate(t *testing.T) {
	type fields struct {
		flagEnableMultiCluster   bool
		flagSecondaryKubeconfig  string
		flagSecondaryKubecontext string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			"enable multi cluster: no error when multi cluster is disabled",
			fields{
				flagEnableMultiCluster:   false,
				flagSecondaryKubeconfig:  "",
				flagSecondaryKubecontext: "",
			},
			false,
		},
		{
			"enable multi cluster: errors when both secondary kubeconfig and kubecontext are empty",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "",
				flagSecondaryKubecontext: "",
			},
			true,
		},
		{
			"enable multi cluster: no error when secondary kubeconfig but not kubecontext is provided",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "foo",
				flagSecondaryKubecontext: "",
			},
			false,
		},
		{
			"enable multi cluster: no error when secondary kubecontext but not kubeconfig is provided",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "",
				flagSecondaryKubecontext: "foo",
			},
			false,
		},
		{
			"enable multi cluster: no error when both secondary kubecontext and kubeconfig are provided",
			fields{
				flagEnableMultiCluster:   true,
				flagSecondaryKubeconfig:  "foo",
				flagSecondaryKubecontext: "bar",
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tf := &TestFlags{
				flagEnableMultiCluster:   tt.fields.flagEnableMultiCluster,
				flagSecondaryKubeconfig:  tt.fields.flagSecondaryKubeconfig,
				flagSecondaryKubecontext: tt.fields.flagSecondaryKubecontext,
			}
			err := tf.validate()
			if tt.wantErr {
				require.EqualError(t, err, "at least one of -secondary-kubecontext or -secondary-kubeconfig flags must be provided if -enable-multi-cluster is set")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
