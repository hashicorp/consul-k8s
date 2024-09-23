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

	meshv2beta1 "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
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
				flagTolerations: `- "operator": "Equal"
  "effect": "NoSchedule"
  "key": "node"
  "value": "clients"
- "operator": "Equal"
  "effect": "NoSchedule"
  "key": "node2"
  "value": "clients2"`,
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
		meshGWConfigFileExists     bool
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
		"mesh gw config file exists": {
			meshGWConfigFileExists: true,
		},
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
			if tt.meshGWConfigFileExists {
				configFileName = createGatewayConfigFile(t, validGWConfigurationKitchenSink, "config.yaml")
			}

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

var validGWConfigurationKitchenSink = `gatewayClassConfigs:
- apiVersion: mesh.consul.hashicorp.com/v2beta1
  kind: GatewayClassConfig
  metadata:
    name: consul-mesh-gateway
  spec:
    deployment:
      hostNetwork: true
      dnsPolicy: ClusterFirst
      replicas:
        min: 3
        default: 3
        max: 3
      nodeSelector:
        beta.kubernetes.io/arch: amd64
        beta.kubernetes.io/os: linux
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: consul
                  release: consul-helm
                  component: mesh-gateway
              topologyKey: kubernetes.io/hostname
      tolerations:
        - key: "key1"
          operator: "Equal"
          value: "value1"
          effect: "NoSchedule"
      container:
        portModifier: 8000
        resources:
          requests:
            cpu: 200m
            memory: 200Mi
          limits:
            cpu: 200m
            memory: 200Mi
meshGateways:
- apiVersion: mesh.consul.hashicorp.com/v2beta1
  kind: MeshGateway
  metadata:
    name: mesh-gateway
    namespace: consul
  spec:
    gatewayClassName: consul-mesh-gateway
`

var validGWConfigurationMinimal = `gatewayClassConfigs:
- apiVersion: mesh.consul.hashicorp.com/v2beta1
  kind: GatewayClassConfig
  metadata:
    name: consul-mesh-gateway
  spec:
    deployment:
meshGateways:
- apiVersion: mesh.consul.hashicorp.com/v2beta1
  kind: MeshGateway
  metadata:
    name: mesh-gateway
    namespace: consul
  spec:
    gatewayClassName: consul-mesh-gateway
`

var invalidGWConfiguration = `
gatewayClassConfigs:
iVersion= mesh.consul.hashicorp.com/v2beta1
  kind: gatewayClassConfig
  metadata:
    name: consul-mesh-gateway
    namespace: namespace
  spec:
    deployment:
      resources:
        requests:
          cpu: 100m
meshGateways:
- name: mesh-gateway
  spec:
    gatewayClassName: consul-mesh-gateway
`

func TestRun_loadGatewayConfigs(t *testing.T) {
	var replicasCount int32 = 3
	testCases := map[string]struct {
		config             string
		filename           string
		expectedDeployment v2beta1.GatewayClassDeploymentConfig
	}{
		"kitchen sink": {
			config:   validGWConfigurationKitchenSink,
			filename: "kitchenSinkConfig.yaml",
			expectedDeployment: v2beta1.GatewayClassDeploymentConfig{
				HostNetwork: true,
				DNSPolicy:   "ClusterFirst",
				NodeSelector: map[string]string{
					"beta.kubernetes.io/arch": "amd64",
					"beta.kubernetes.io/os":   "linux",
				},
				Replicas: &v2beta1.GatewayClassReplicasConfig{
					Default: &replicasCount,
					Min:     &replicasCount,
					Max:     &replicasCount,
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "key1",
						Operator: "Equal",
						Value:    "value1",
						Effect:   "NoSchedule",
					},
				},

				Affinity: &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app":       "consul",
										"release":   "consul-helm",
										"component": "mesh-gateway",
									},
								},
								TopologyKey: "kubernetes.io/hostname",
							},
						},
					},
				},
				Container: &v2beta1.GatewayClassContainerConfig{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("200Mi"),
							corev1.ResourceCPU:    resource.MustParse("200m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("200Mi"),
							corev1.ResourceCPU:    resource.MustParse("200m"),
						},
					},
					PortModifier: 8000,
				},
			},
		},
		"minimal configuration": {
			config:   validGWConfigurationMinimal,
			filename: "minimalConfig.yaml",
			expectedDeployment: v2beta1.GatewayClassDeploymentConfig{
				Container: &v2beta1.GatewayClassContainerConfig{
					Resources: &defaultResourceRequirements,
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			filename := createGatewayConfigFile(t, tc.config, tc.filename)
			// setup k8s client
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			client := fake.NewClientBuilder().WithScheme(s).Build()

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                        ui,
				k8sClient:                 client,
				flagGatewayConfigLocation: filename,
			}

			err := cmd.loadGatewayConfigs()
			require.NoError(t, err)
			require.NotEmpty(t, cmd.gatewayConfig.GatewayClassConfigs)
			require.NotEmpty(t, cmd.gatewayConfig.MeshGateways)

			// we only created one class config
			classConfig := cmd.gatewayConfig.GatewayClassConfigs[0].DeepCopy()

			expectedClassConfig := v2beta1.GatewayClassConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v2beta1.MeshGroupVersion.String(),
					Kind:       v2beta1.KindGatewayClassConfig,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-mesh-gateway",
				},
				Spec: v2beta1.GatewayClassConfigSpec{
					Deployment: tc.expectedDeployment,
				},
				Status: v2beta1.Status{},
			}
			require.Equal(t, expectedClassConfig.DeepCopy(), classConfig)

			// check mesh gateway, we only created one of these
			actualMeshGateway := cmd.gatewayConfig.MeshGateways[0]

			expectedMeshGateway := &v2beta1.MeshGateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "MeshGateway",
					APIVersion: v2beta1.MeshGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mesh-gateway",
					Namespace: "consul",
				},
				Spec: meshv2beta1.MeshGateway{
					GatewayClassName: "consul-mesh-gateway",
				},
			}

			require.Equal(t, expectedMeshGateway.DeepCopy(), actualMeshGateway)
		})
	}
}

func TestRun_loadGatewayConfigsWithInvalidFile(t *testing.T) {
	filename := createGatewayConfigFile(t, invalidGWConfiguration, "config.yaml")
	// setup k8s client
	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	client := fake.NewClientBuilder().WithScheme(s).Build()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                        ui,
		k8sClient:                 client,
		flagGatewayConfigLocation: filename,
	}

	err := cmd.loadGatewayConfigs()
	require.Error(t, err)
	require.Empty(t, cmd.gatewayConfig.GatewayClassConfigs)
	require.Empty(t, cmd.gatewayConfig.MeshGateways)
}

func TestRun_loadGatewayConfigsWhenConfigFileDoesNotExist(t *testing.T) {
	filename := "./consul/config/config.yaml"
	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	client := fake.NewClientBuilder().WithScheme(s).Build()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                        ui,
		k8sClient:                 client,
		flagGatewayConfigLocation: filename,
	}

	err := cmd.loadGatewayConfigs()
	require.NoError(t, err)
	require.Empty(t, cmd.gatewayConfig.GatewayClassConfigs)
	require.Empty(t, cmd.gatewayConfig.MeshGateways)
	require.Contains(t, string(ui.ErrorWriter.Bytes()), "gateway configuration file not found, skipping gateway configuration")
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
