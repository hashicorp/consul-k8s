// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatewayresources

import (
	"os"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestRun_flagValidation(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		cmd         *Command
		expectedErr string
	}{
		"required gateway class config name": {
			cmd:         &Command{},
			expectedErr: "-gateway-class-config-name must be set",
		},
		"required gateway class name": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
			},
			expectedErr: "-gateway-class-name must be set",
		},
		"required heritage": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
			},
			expectedErr: "-heritage must be set",
		},
		"required chart": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
			},
			expectedErr: "-chart must be set",
		},
		"required app": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
			},
			expectedErr: "-app must be set",
		},
		"required release": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
			},
			expectedErr: "-release-name must be set",
		},
		"required component": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
				flagRelease:                "test",
			},
			expectedErr: "-component must be set",
		},
		"required controller name": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
				flagRelease:                "test",
				flagComponent:              "test",
			},
			expectedErr: "-controller-name must be set",
		},
		"required valid tolerations": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
				flagRelease:                "test",
				flagComponent:              "test",
				flagControllerName:         "test",
				flagTolerations:            "foo",
			},
			expectedErr: "error decoding tolerations: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `foo` into []gatewayresources.toleration",
		},
		"required valid nodeSelector": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
				flagRelease:                "test",
				flagComponent:              "test",
				flagControllerName:         "test",
				flagNodeSelector:           "foo",
			},
			expectedErr: "error decoding node selector: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `foo` into map[string]string",
		},
		"required valid service annotations": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
				flagRelease:                "test",
				flagComponent:              "test",
				flagControllerName:         "test",
				flagServiceAnnotations:     "foo",
			},
			expectedErr: "error decoding service annotations: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `foo` into []string",
		},
		"valid without optional flags": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
				flagRelease:                "test",
				flagComponent:              "test",
				flagControllerName:         "test",
			},
		},
		"valid with optional flags": {
			cmd: &Command{
				flagGatewayClassConfigName: "test",
				flagGatewayClassName:       "test",
				flagHeritage:               "test",
				flagChart:                  "test",
				flagApp:                    "test",
				flagRelease:                "test",
				flagComponent:              "test",
				flagControllerName:         "test",
				flagNodeSelector: `
foo: 1
bar: 2`,
				flagTolerations: `
- value: foo
- value: bar`,
				flagServiceAnnotations: `
- foo
- bar`,
				flagOpenshiftSCCName: "restricted-v2",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt := tt

			t.Parallel()

			err := tt.cmd.validateFlags()
			if tt.expectedErr == "" && err != nil {
				t.Errorf("unexpected error occured: %v", err)
			}
			if tt.expectedErr != "" && err == nil {
				t.Error("expected error but got none")
			}
			if tt.expectedErr != "" {
				require.EqualError(t, err, tt.expectedErr)
			}
		})
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		existingGatewayClass       bool
		existingGatewayClassConfig bool
	}{
		"both exist": {
			existingGatewayClass:       true,
			existingGatewayClassConfig: true,
		},
		"api gateway class config doesn't exist": {
			existingGatewayClass: true,
		},
		"api gateway class doesn't exist": {
			existingGatewayClassConfig: true,
		},
		"neither exist": {},
	} {
		t.Run(name, func(t *testing.T) {
			tt := tt

			t.Parallel()

			existingGatewayClassConfig := &v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			}
			existingGatewayClass := &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			}

			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			configFileName := gatewayConfigFilename

			objs := []client.Object{}
			if tt.existingGatewayClass {
				objs = append(objs, existingGatewayClass)
			}
			if tt.existingGatewayClassConfig {
				objs = append(objs, existingGatewayClassConfig)
			}

			client := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                        ui,
				k8sClient:                 client,
				flagGatewayConfigLocation: configFileName,
			}

			code := cmd.Run([]string{
				"-gateway-class-config-name", "test",
				"-gateway-class-name", "test",
				"-heritage", "test",
				"-chart", "test",
				"-app", "test",
				"-release-name", "test",
				"-component", "test",
				"-controller-name", "test",
				"-openshift-scc-name", "restricted-v2",
			})

			require.Equal(t, 0, code)
		})
	}
}

var validResourceConfiguration = `{
    "requests": {
        "memory": "200Mi",
        "cpu": "200m"
    },
    "limits": {
        "memory": "200Mi",
        "cpu": "200m"
    }
}
`

var invalidResourceConfiguration = `{"resources":
{
	"memory": "100Mi"
        "cpu": "100m"
    },
    "limits": {
	"memory": "100Mi"
        "cpu": "100m"
    },
}
`

func TestRun_loadResourceConfig(t *testing.T) {
	filename := createGatewayConfigFile(t, validResourceConfiguration, "resource.json")
	// setup k8s client
	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	client := fake.NewClientBuilder().WithScheme(s).Build()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: client,
	}

	expectedResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("200Mi"),
			corev1.ResourceCPU:    resource.MustParse("200m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("200Mi"),
			corev1.ResourceCPU:    resource.MustParse("200m"),
		},
	}

	resources, err := cmd.loadResourceConfig(filename)
	require.NoError(t, err)
	require.Equal(t, resources, expectedResources)
}

func TestRun_loadResourceConfigInvalidConfigFile(t *testing.T) {
	filename := createGatewayConfigFile(t, invalidResourceConfiguration, "resource.json")
	// setup k8s client
	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	client := fake.NewClientBuilder().WithScheme(s).Build()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: client,
	}

	_, err := cmd.loadResourceConfig(filename)
	require.Error(t, err)
}

func TestRun_loadResourceConfigFileWhenConfigFileDoesNotExist(t *testing.T) {
	filename := "./consul/config/resources.json"
	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	client := fake.NewClientBuilder().WithScheme(s).Build()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: client,
	}

	resources, err := cmd.loadResourceConfig(filename)
	require.NoError(t, err)
	require.Equal(t, resources, defaultResourceRequirements) // should be using defaults
	require.Contains(t, string(ui.OutputWriter.Bytes()), "No resources.json found, using defaults")
}

func createGatewayConfigFile(t *testing.T, fileContent, filename string) string {
	t.Helper()

	// create a temp file to store configuration yaml
	tmpdir := t.TempDir()
	file, err := os.CreateTemp(tmpdir, filename)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	_, err = file.WriteString(fileContent)
	if err != nil {
		t.Fatal(err)
	}

	return file.Name()
}
