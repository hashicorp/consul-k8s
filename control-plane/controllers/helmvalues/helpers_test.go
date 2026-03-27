package helmvalues

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConsulFullName(t *testing.T) {
	cases := []struct {
		name     string
		hv       *HelmValues
		expected string
	}{
		{
			name: "fullNameOverride takes precedence",
			hv: &HelmValues{
				FullNameOverride: "my-custom-consul",
				Global:           GlobalConfig{Name: "global-name"},
				NameOverride:     "name-override",
				Release:          ReleaseConfig{Name: "release"},
			},
			expected: "my-custom-consul",
		},
		{
			name: "global name takes precedence over nameOverride",
			hv: &HelmValues{
				Global:       GlobalConfig{Name: "global-consul"},
				NameOverride: "name-override",
				Release:      ReleaseConfig{Name: "release"},
			},
			expected: "global-consul",
		},
		{
			name: "nameOverride with release name",
			hv: &HelmValues{
				NameOverride: "custom",
				Release:      ReleaseConfig{Name: "my-release"},
			},
			expected: "my-release-custom",
		},
		{
			name: "default consul name with release",
			hv: &HelmValues{
				Release: ReleaseConfig{Name: "my-release"},
			},
			expected: "my-release-consul",
		},
		{
			name: "truncates to 63 characters",
			hv: &HelmValues{
				FullNameOverride: "this-is-a-very-long-name-that-exceeds-sixty-three-characters-limit-here",
			},
			expected: "this-is-a-very-long-name-that-exceeds-sixty-three-characters-li",
		},
		{
			name: "trims trailing hyphens after truncation",
			hv: &HelmValues{
				FullNameOverride: "this-is-a-very-long-name-that-exceeds-sixty-three-characters----",
			},
			expected: "this-is-a-very-long-name-that-exceeds-sixty-three-characters",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ConsulFullName(tc.hv)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestTruncateAndTrim(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "no truncation needed",
			input:    "short",
			maxLen:   10,
			expected: "short",
		},
		{
			name:     "exact length",
			input:    "exactly10c",
			maxLen:   10,
			expected: "exactly10c",
		},
		{
			name:     "truncates long string",
			input:    "this-is-too-long",
			maxLen:   10,
			expected: "this-is-to",
		},
		{
			name:     "trims trailing hyphens",
			input:    "test---",
			maxLen:   10,
			expected: "test",
		},
		{
			name:     "truncates then trims hyphens",
			input:    "test-name---extra",
			maxLen:   10,
			expected: "test-name",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateAndTrim(tc.input, tc.maxLen)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestTrimSuffix(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		suffix   string
		expected string
	}{
		{
			name:     "single trailing hyphen",
			input:    "test-",
			suffix:   "-",
			expected: "test",
		},
		{
			name:     "multiple trailing hyphens",
			input:    "test---",
			suffix:   "-",
			expected: "test",
		},
		{
			name:     "no trailing suffix",
			input:    "test",
			suffix:   "-",
			expected: "test",
		},
		{
			name:     "empty string",
			input:    "",
			suffix:   "-",
			expected: "",
		},
		{
			name:     "empty suffix",
			input:    "test-",
			suffix:   "",
			expected: "test-",
		},
		{
			name:     "all hyphens",
			input:    "---",
			suffix:   "-",
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := trimSuffix(tc.input, tc.suffix)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestGetHelmValues(t *testing.T) {
	validJSON := `{
		"global": {
			"datacenter": "dc1",
			"name": "consul"
		},
		"release": {
			"name": "consul",
			"namespace": "consul-ns"
		}
	}`

	cases := []struct {
		name          string
		releaseName   string
		namespace     string
		configMap     *corev1.ConfigMap
		expectError   bool
		errorContains string
		validate      func(t *testing.T, hv *HelmValues)
	}{
		{
			name:        "successful retrieval",
			releaseName: "consul",
			namespace:   "consul-ns",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "consul-consul-helm-values",
					Namespace: "consul-ns",
				},
				Data: map[string]string{
					"values.json": validJSON,
				},
			},
			expectError: false,
			validate: func(t *testing.T, hv *HelmValues) {
				require.Equal(t, "dc1", hv.Global.Datacenter)
				require.Equal(t, "consul", hv.Global.Name)
			},
		},
		{
			name:          "configmap not found",
			releaseName:   "consul",
			namespace:     "consul-ns",
			configMap:     nil,
			expectError:   true,
			errorContains: "failed to get helm values configmap",
		},
		{
			name:        "values.json key missing",
			releaseName: "consul",
			namespace:   "consul-ns",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "consul-consul-helm-values",
					Namespace: "consul-ns",
				},
				Data: map[string]string{
					"other-key": "some-data",
				},
			},
			expectError:   true,
			errorContains: "values.json not found",
		},
		{
			name:        "invalid JSON",
			releaseName: "consul",
			namespace:   "consul-ns",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "consul-consul-helm-values",
					Namespace: "consul-ns",
				},
				Data: map[string]string{
					"values.json": "not-valid-json",
				},
			},
			expectError:   true,
			errorContains: "failed to unmarshal helm values",
		},
		{
			name:        "custom release name",
			releaseName: "my-consul",
			namespace:   "default",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-consul-consul-helm-values",
					Namespace: "default",
				},
				Data: map[string]string{
					"values.json": validJSON,
				},
			},
			expectError: false,
			validate: func(t *testing.T, hv *HelmValues) {
				require.NotNil(t, hv)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder()
			if tc.configMap != nil {
				clientBuilder.WithObjects(tc.configMap)
			}
			k8sClient := clientBuilder.Build()

			result, err := GetHelmValues(context.Background(), k8sClient, tc.releaseName, tc.namespace)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorContains)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				if tc.validate != nil {
					tc.validate(t, result)
				}
			}
		})
	}
}
