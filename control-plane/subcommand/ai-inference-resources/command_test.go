// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aiinferenceresources

import (
	"context"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// scheme shared by all tests.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	return s
}

// fullCmd returns a Command with every required flag set so individual tests
// only need to override the one flag under test.
func fullCmd() *Command {
	return &Command{
		flagConfigName:        "consul-ai-gateway",
		flagHeritage:          "Helm",
		flagChart:             "consul-helm",
		flagApp:               "consul",
		flagRelease:           "test",
		flagComponent:         "ai-inference-model",
		flagEnabled:           true,
		flagInterceptorPort:   21101,
		flagInferencePath:     "/v1",
		flagInferenceProtocol: "openai",
		flagRequestsMemory:    "256Mi",
		flagRequestsCPU:       "500m",
		flagLimitsMemory:      "512Mi",
		flagLimitsCPU:         "1000m",
	}
}

// ---------------------------------------------------------------------------
// Flag validation
// ---------------------------------------------------------------------------

func TestRun_flagValidation(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate      func(*Command)
		expectedErr string
	}{
		"config-name required": {
			mutate:      func(c *Command) { c.flagConfigName = "" },
			expectedErr: "-config-name must be set",
		},
		"heritage required": {
			mutate:      func(c *Command) { c.flagHeritage = "" },
			expectedErr: "-heritage must be set",
		},
		"chart required": {
			mutate:      func(c *Command) { c.flagChart = "" },
			expectedErr: "-chart must be set",
		},
		"app required": {
			mutate:      func(c *Command) { c.flagApp = "" },
			expectedErr: "-app must be set",
		},
		"release required": {
			mutate:      func(c *Command) { c.flagRelease = "" },
			expectedErr: "-release-name must be set",
		},
		"invalid protocol": {
			mutate:      func(c *Command) { c.flagInferenceProtocol = "grpc" },
			expectedErr: `-inference-protocol must be one of openai|anthropic|bedrock, got "grpc"`,
		},
		"port too low": {
			mutate:      func(c *Command) { c.flagInterceptorPort = 80 },
			expectedErr: "-interceptor-port must be between 1024 and 65535, got 80",
		},
		"port too high": {
			mutate:      func(c *Command) { c.flagInterceptorPort = 70000 },
			expectedErr: "-interceptor-port must be between 1024 and 65535, got 70000",
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmd := fullCmd()
			tt.mutate(cmd)
			err := cmd.validateFlags()
			require.EqualError(t, err, tt.expectedErr)
		})
	}
}

// ---------------------------------------------------------------------------
// forceInferenceModelConfig — upsert
// ---------------------------------------------------------------------------

func TestForceInferenceModelConfig_create(t *testing.T) {
	t.Parallel()

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := fullCmd()
	resources, err := cmd.buildResources()
	require.NoError(t, err)

	desired := &v1alpha1.InferenceModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: cmd.flagConfigName},
		Spec: v1alpha1.InferenceModelConfigSpec{
			Enabled: cmd.flagEnabled,
			Defaults: v1alpha1.InferenceModelDefaults{
				InterceptorPort:   int32(cmd.flagInterceptorPort),
				InferencePath:     cmd.flagInferencePath,
				InferenceProtocol: cmd.flagInferenceProtocol,
				Resources:         resources,
			},
		},
	}

	require.NoError(t, forceInferenceModelConfig(context.Background(), fc, desired))

	got := &v1alpha1.InferenceModelConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-gateway"}, got))
	require.Equal(t, int32(21101), got.Spec.Defaults.InterceptorPort)
	require.Equal(t, "openai", got.Spec.Defaults.InferenceProtocol)
	require.True(t, got.Spec.Enabled)
}

func TestForceInferenceModelConfig_update(t *testing.T) {
	t.Parallel()

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := fullCmd()
	resources, _ := cmd.buildResources()

	// Seed initial object.
	existing := &v1alpha1.InferenceModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: cmd.flagConfigName},
		Spec: v1alpha1.InferenceModelConfigSpec{
			Enabled: cmd.flagEnabled,
			Defaults: v1alpha1.InferenceModelDefaults{
				InterceptorPort:   int32(cmd.flagInterceptorPort),
				InferencePath:     cmd.flagInferencePath,
				InferenceProtocol: cmd.flagInferenceProtocol,
				Resources:         resources,
			},
		},
	}
	require.NoError(t, fc.Create(context.Background(), existing))

	// Upgrade: change protocol and port.
	cmd.flagInferenceProtocol = "anthropic"
	cmd.flagInterceptorPort = 21200
	resources, _ = cmd.buildResources()

	updated := existing.DeepCopy()
	updated.Spec.Defaults.InferenceProtocol = cmd.flagInferenceProtocol
	updated.Spec.Defaults.InterceptorPort = int32(cmd.flagInterceptorPort)
	updated.Spec.Defaults.Resources = resources

	require.NoError(t, forceInferenceModelConfig(context.Background(), fc, updated))

	got := &v1alpha1.InferenceModelConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-gateway"}, got))
	require.Equal(t, int32(21200), got.Spec.Defaults.InterceptorPort)
	require.Equal(t, "anthropic", got.Spec.Defaults.InferenceProtocol)
}

// ---------------------------------------------------------------------------
// Full Run() integration via CLI flags
// ---------------------------------------------------------------------------

func TestRun_success(t *testing.T) {
	t.Parallel()

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}

	args := []string{
		"-config-name=consul-ai-gateway",
		"-heritage=Helm",
		"-chart=consul-helm",
		"-app=consul",
		"-release-name=test",
		"-enabled=true",
		"-interceptor-port=21101",
		"-inference-path=/v1",
		"-inference-protocol=openai",
		"-requests-memory=256Mi",
		"-requests-cpu=500m",
		"-limits-memory=512Mi",
		"-limits-cpu=1000m",
	}
	require.Equal(t, 0, cmd.Run(args))

	got := &v1alpha1.InferenceModelConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-gateway"}, got))
	require.True(t, got.Spec.Enabled)
	require.Equal(t, int32(21101), got.Spec.Defaults.InterceptorPort)
	require.Equal(t, "/v1", got.Spec.Defaults.InferencePath)
	require.Equal(t, "openai", got.Spec.Defaults.InferenceProtocol)
	require.Equal(t, "256Mi", got.Spec.Defaults.Resources.Requests.Memory().String())
	require.Equal(t, "500m", got.Spec.Defaults.Resources.Requests.Cpu().String())
	require.Equal(t, "512Mi", got.Spec.Defaults.Resources.Limits.Memory().String())
	// k8s normalises 1000m → "1"
	require.Equal(t, "1", got.Spec.Defaults.Resources.Limits.Cpu().String())
}

func TestRun_missingRequiredFlags_returnsOne(t *testing.T) {
	t.Parallel()
	cmd := &Command{UI: cli.NewMockUi(), ctx: context.Background()}
	require.Equal(t, 1, cmd.Run([]string{"-enabled=true"}))
}

// ---------------------------------------------------------------------------
// buildResources — bad quantity strings
// ---------------------------------------------------------------------------

func TestBuildResources_invalidQuantity(t *testing.T) {
	t.Parallel()
	cmd := fullCmd()
	cmd.flagRequestsMemory = "not-a-quantity"
	_, err := cmd.buildResources()
	require.Error(t, err)
	require.Contains(t, err.Error(), "requests-memory")
}
