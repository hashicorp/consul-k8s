// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package generatemanifests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/assert/yaml"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestRun_flagValidation(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		cmd         *Command
		expectedErr string
	}{

		"required chart": {
			cmd: &Command{
				flagApp:     "test",
				flagRelease: "test",
			},
			expectedErr: "-chart must be set",
		},
		"required app": {
			cmd: &Command{

				flagRelease: "test",
				flagChart:   "test",
			},
			expectedErr: "-app must be set",
		},
		"required release": {
			cmd: &Command{
				flagChart: "test",
				flagApp:   "test",
			},
			expectedErr: "-release-name must be set",
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

	s := runtime.NewScheme()
	require.NoError(t, gwv1.Install(s))
	require.NoError(t, gwv1alpha2.Install(s))
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	for name, tt := range map[string]struct {
		client          client.Client
		addToSchemeFail bool
	}{
		"both exist": {
			client: fake.NewClientBuilder().WithScheme(s).Build(),
		},
		"api gateway class config doesn't exist": {
			client: fake.NewClientBuilder().WithScheme(s).Build(),
		},
		"api gateway class doesn't exist": {
			client: fake.NewClientBuilder().WithScheme(s).Build(),
		},
		"neither exist": {
			client: fake.NewClientBuilder().WithScheme(s).Build(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt := tt

			t.Parallel()

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				k8sClient: tt.client,
			}
			// if tt.addToSchemeFail {
			// 	cmd.AddToSchemeFunc = func(_ *runtime.Scheme) error {
			// 		return fmt.Errorf("mock AddToScheme failure")
			// 	}
			// }
			cmd.init()
			cmd.flagManifestsGatewayAPIDir = t.TempDir()
			code := cmd.Run([]string{
				"-chart", "test",
				"-app", "test",
				"-release-name", "test",
				"-openshift-scc-name", "restricted-v2",
				"-manifests-gatewayapi-dir", cmd.flagManifestsGatewayAPIDir,
			})

			if tt.addToSchemeFail {
				require.NotEqual(t, 0, code)
				require.Contains(t, ui.ErrorWriter.String(), "Could not add client-go schema")
			} else {
				require.Equal(t, 0, code)
			}
		})
	}
}

func TestDumpedAPIObjectsNoK8sClient(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	require.NoError(t, gwv1.Install(s))
	require.NoError(t, gwv1alpha2.Install(s))

	// test case to check the k8s client is not nil
	cmd := Command{
		k8sClient: nil,
	}

	err := cmd.dumpGatewayAPIObjects()
	require.EqualError(t, err, "k8s client is nil")

}

func TestDumpedAPIObjects(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	require.NoError(t, gwv1.Install(s))
	require.NoError(t, gwv1alpha2.Install(s))

	ui := cli.NewMockUi()
	// test case to check the k8s client is not nil
	cmd := Command{
		k8sClient:                  fake.NewClientBuilder().WithScheme(s).Build(),
		flagManifestsGatewayAPIDir: t.TempDir(),
		UI:                         ui,
	}

	err := cmd.dumpGatewayAPIObjects()
	require.NoError(t, err)
}

func TestDumpedAPIObjectsv1beta(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	//require.NoError(t, gwv1.Install(s))
	require.NoError(t, gwv1alpha2.Install(s))

	ui := cli.NewMockUi()
	// test case to check the k8s client is not nil
	cmd := Command{
		k8sClient:                  fake.NewClientBuilder().WithScheme(s).Build(),
		flagManifestsGatewayAPIDir: t.TempDir(),
		UI:                         ui,
	}

	err := cmd.dumpGatewayAPIObjects()
	require.NoError(t, err)
}

func TestDumpedAPIObjects_WithGatewayPolicy(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	require.NoError(t, gwv1.Install(s))
	require.NoError(t, gwv1alpha2.Install(s))

	// pre-seed a GatewayPolicy so extractItems iterates over actual items
	policy := &v1alpha1.GatewayPolicy{}
	policy.Name = "my-policy"
	policy.Namespace = "default"
	policy.Spec.TargetRef = v1alpha1.PolicyTargetReference{
		Group: "gateway.networking.k8s.io/v1beta1",
		Kind:  "Gateway",
		Name:  "my-gateway",
	}

	ui := cli.NewMockUi()
	cmd := Command{
		k8sClient:                  fake.NewClientBuilder().WithScheme(s).WithObjects(policy).Build(),
		flagManifestsGatewayAPIDir: t.TempDir(),
		UI:                         ui,
	}

	err := cmd.dumpGatewayAPIObjects()
	require.NoError(t, err)
}

func TestDumpedAPIObjects_GatewayPolicySkippedOnListError(t *testing.T) {
	t.Parallel()

	// omit v1alpha1 from scheme so GatewayPolicyList.List fails — covers the
	// Skipping GatewayPolicy dump error-log branch
	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, gwv1.Install(s))
	require.NoError(t, gwv1alpha2.Install(s))
	// v1alpha1 intentionally NOT added

	ui := cli.NewMockUi()
	cmd := Command{
		k8sClient:                  fake.NewClientBuilder().WithScheme(s).Build(),
		flagManifestsGatewayAPIDir: t.TempDir(),
		UI:                         ui,
	}

	// dumpGatewayAPIObjects should still succeed; the GatewayPolicy error is logged not returned
	err := cmd.dumpGatewayAPIObjects()
	require.NoError(t, err)
	require.Contains(t, ui.OutputWriter.String(), "Skipping GatewayPolicy dump")
}

func TestEnforceGatewayAPIVersion(t *testing.T) {

	cases := []struct {
		name      string
		kind      string
		APIGroup  string
		wantGroup string
	}{

		{"Gateway", "Gateway", "gateway.networking.k8s.io/v1beta1", "gateway.networking.k8s.io/v1"},
		{"HTTPRoute", "HTTPRoute", "gateway.networking.k8s.io/v1beta1", "gateway.networking.k8s.io/v1"},
		{"GRPCRoute", "GRPCRoute", "gateway.networking.k8s.io/v1beta1", "gateway.networking.k8s.io/v1"},
		{"ReferenceGrant", "ReferenceGrant", "gateway.networking.k8s.io/v1beta1", "gateway.networking.k8s.io/v1beta1"},
		{"UDPRoute", "UDPRoute", "gateway.networking.k8s.io/v1alpha2", "gateway.networking.k8s.io/v1alpha2"},
		{"TLSRoute", "TLSRoute", "gateway.networking.k8s.io/v1alpha2", "gateway.networking.k8s.io/v1alpha2"},
		{"TCPRoute", "TCPRoute", "gateway.networking.k8s.io/v1alpha2", "gateway.networking.k8s.io/v1alpha2"},
		{"GatewayPolicy", "GatewayPolicy", "gateway.networking.k8s.io/v1beta1", "gateway.networking.k8s.io/v1beta1"},

		{"EmptyKind", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{
				"kind":       tc.kind,
				"apiVersion": tc.APIGroup,
				"spec": map[string]interface{}{
					"targetRef": map[string]interface{}{
						"group": tc.APIGroup,
					},
				},
			}
			enforceGatewayAPIVersion(raw)
			got, _ := raw["apiVersion"].(string)
			require.Equal(t, tc.wantGroup, got)
		})
	}
}

func TestEnforceConsulAPIVersion(t *testing.T) {

	cases := []struct {
		name               string
		kind               string
		setupGateway       bool
		setupParent        bool
		wantGroup          string
		isConsulControlled bool
	}{
		// ======================
		// Gateway
		// ======================
		{
			name:               "Gateway - consul controlled",
			kind:               "Gateway",
			setupGateway:       true,
			wantGroup:          "consul.hashicorp.com/v1beta1",
			isConsulControlled: true,
		},
		{
			name:               "Gateway - NOT consul controlled",
			kind:               "Gateway",
			setupGateway:       false,
			wantGroup:          "", // no change
			isConsulControlled: false,
		},

		// ======================
		// Routes
		// ======================
		{
			name:               "HTTPRoute - consul parent",
			kind:               "HTTPRoute",
			setupGateway:       true,
			setupParent:        true,
			wantGroup:          "consul.hashicorp.com/v1beta1",
			isConsulControlled: true,
		},
		{
			name:               "HTTPRoute - no consul parent",
			kind:               "HTTPRoute",
			setupGateway:       false,
			setupParent:        true,
			wantGroup:          "",
			isConsulControlled: false,
		},

		{
			name:               "GRPCRoute - consul parent",
			kind:               "GRPCRoute",
			setupGateway:       true,
			setupParent:        true,
			wantGroup:          "consul.hashicorp.com/v1alpha2",
			isConsulControlled: true,
		},

		{
			name:               "TCPRoute - consul parent",
			kind:               "TCPRoute",
			setupGateway:       true,
			setupParent:        true,
			wantGroup:          "consul.hashicorp.com/v1alpha2",
			isConsulControlled: true,
		},

		// ======================
		// ReferenceGrant
		// ======================
		{
			name:         "ReferenceGrant - gateway consul",
			kind:         "ReferenceGrant",
			setupGateway: true,
			setupParent:  true,
			wantGroup:    "consul.hashicorp.com/v1beta1",
		},
		{
			name:         "ReferenceGrant - no gateway but group rewrite",
			kind:         "ReferenceGrant",
			setupGateway: false,
			setupParent:  false,
			wantGroup:    "consul.hashicorp.com/v1beta1", // your intentional logic
			// but you don't consider this consul controlled
		},
		{
			name:               "GatewayPolicy - targetRef group rewritten to consul",
			kind:               "GatewayPolicy",
			isConsulControlled: true,
			wantGroup:          "consul.hashicorp.com/v1beta1",
		},

		{
			name:      "EmptyKind",
			kind:      "",
			wantGroup: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			// Reset global maps (IMPORTANT)
			gatewayMap = map[string]bool{}

			// Setup gatewayMap if needed
			if tc.setupGateway {
				gatewayMap["default/test"] = true
			}

			// Base object
			raw := map[string]interface{}{
				"kind": tc.kind,
				"metadata": map[string]interface{}{
					"name":      "test",
					"namespace": "default",
				},
				"apiVersion": "gateway.networking.k8s.io/v1beta1",
			}

			// Add spec if needed
			if tc.setupParent {
				raw["spec"] = map[string]interface{}{
					"parentRefs": []interface{}{
						map[string]interface{}{
							"kind":      "Gateway",
							"name":      "test",
							"namespace": "default",
						},
					},
				}
			}

			// Special case for ReferenceGrant
			if tc.kind == "ReferenceGrant" {
				raw["spec"] = map[string]interface{}{
					"from": []interface{}{
						map[string]interface{}{
							"kind":      "Gateway",
							"name":      "test",
							"namespace": "default",
							"group":     "gateway.networking.k8s.io",
						},
					},
				}
			}

			// Special case for GatewayPolicy
			if tc.kind == "GatewayPolicy" {
				raw["spec"] = map[string]interface{}{
					"targetRef": map[string]interface{}{
						"group": "gateway.networking.k8s.io/v1beta1",
						"kind":  "Gateway",
						"name":  "test",
					},
				}
			}

			enforceConsulApiVersion(raw)

			got, _ := raw["apiVersion"].(string)
			t.Logf("Resulting apiVersion: %s", got)
			if tc.isConsulControlled || tc.kind == "ReferenceGrant" {
				if tc.kind == "GatewayPolicy" {
					// GatewayPolicy: check targetRef.group, not apiVersion
					spec := raw["spec"].(map[string]interface{})
					targetRef := spec["targetRef"].(map[string]interface{})
					require.Equal(t, tc.wantGroup, targetRef["group"])
				} else {
					require.Equal(t, tc.wantGroup, got)
				}
			} else {
				require.Equal(t, "gateway.networking.k8s.io/v1beta1", got) // no change expected
			}
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}

// test function for writeObjects to validate the output manifests are in the expected format and have the expected mutations.
func TestWriteObjects(t *testing.T) {
	tmpDir := t.TempDir()
	gatewayMap = map[string]bool{}

	// Setup gatewayMap if needed

	gatewayMap["default/test-gw"] = true

	cmd := &Command{
		flagManifestsGatewayAPIDir: filepath.Join(tmpDir, "gateway"),
		flagManifestsConsulAPIDir:  filepath.Join(tmpDir, "consul"),
		consulApiEnabled:           true,
		UI:                         &cli.MockUi{},
	}

	// test object
	gw := &gwv1.Gateway{}
	gw.APIVersion = "gateway.networking.k8s.io/v1beta1"
	gw.Kind = "Gateway"
	gw.ObjectMeta.Name = "test-gw"
	gw.ObjectMeta.Namespace = "default"
	gw.Spec.Listeners = []gwv1.Listener{
		{
			Name:     "http",
			Port:     80,
			Protocol: gwv1.HTTPProtocolType,
			AllowedRoutes: &gwv1.AllowedRoutes{
				Namespaces: &gwv1.RouteNamespaces{
					From: ptr(gwv1.NamespacesFromAll),
				},
			},
		},
		{
			Name:     "https",
			Port:     443,
			Protocol: gwv1.HTTPSProtocolType,
		},
	}

	objs := []client.Object{gw}

	err := cmd.writeObjects("gateway", objs)
	require.NoError(t, err)

	// check gateway file
	gatewayDir := filepath.Join(tmpDir, "gateway", "gateway")

	files, err := os.ReadDir(gatewayDir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	gatewayFile := filepath.Join(gatewayDir, files[0].Name())

	content, err := os.ReadFile(gatewayFile)
	require.NoError(t, err)

	var gatewayObj map[string]interface{}
	err = yaml.Unmarshal(content, &gatewayObj)
	require.NoError(t, err)

	// validate gateway mutation
	require.Equal(t, "gateway.networking.k8s.io/v1", gatewayObj["apiVersion"])

	// check consul file
	consulDir := filepath.Join(tmpDir, "consul", "gateway")

	files, err = os.ReadDir(consulDir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	consulFile := filepath.Join(consulDir, files[0].Name())
	t.Logf("Checking consulDir: %s", consulDir)

	_, err = os.Stat(consulDir)
	require.NoError(t, err, "consulDir should exist")
	//list file in consulDir for debugging
	consulFiles, err := os.ReadDir(consulDir)
	require.NoError(t, err, "should be able to read consulDir")
	for _, f := range consulFiles {
		t.Logf("Found file in consulDir: %s", f.Name())
	}
	content, err = os.ReadFile(consulFile)
	require.NoError(t, err)

	// print content for debugging
	t.Logf("Content of consul file:\n%s", string(content))

	var consulObj map[string]interface{}
	err = yaml.Unmarshal(content, &consulObj)
	require.NoError(t, err)

	// validate consul mutation
	require.Equal(t, "consul.hashicorp.com/v1beta1", consulObj["apiVersion"])

	md := consulObj["metadata"].(map[string]interface{})
	require.Equal(t, "test-gw-consul", md["name"])
	spec := consulObj["spec"].(map[string]interface{})
	listeners := spec["listeners"].([]interface{})
	require.Len(t, listeners, 2)
	// validate first listener
	l1 := listeners[0].(map[string]interface{})
	require.EqualValues(t, 80, l1["port"])

	// nested validation (this hits deepCopySlice + deepCopyMap)
	ar := l1["allowedRoutes"].(map[string]interface{})
	ns := ar["namespaces"].(map[string]interface{})
	require.Equal(t, "All", ns["from"])

	// validate second listener
	l2 := listeners[1].(map[string]interface{})
	require.EqualValues(t, 443, l2["port"])
	require.Equal(t, "https", l2["name"])
	require.Equal(t, "consul-custom-class", spec["gatewayClassName"])
}

func TestConvertGatewayPolicyTargetRef(t *testing.T) {
	// convertGatewayPolicyTargetRef is always called with objects fetched from
	// the live cluster where targetRef.group is gateway.networking.k8s.io/v1beta1
	// (pre-graduation customer state). It unconditionally rewrites it to v1.
	cases := []struct {
		name          string
		inputGroup    string
		wantTargetRef string
	}{
		{
			name:          "v1beta1 rewritten to v1",
			inputGroup:    "gateway.networking.k8s.io/v1beta1",
			wantTargetRef: "gateway.networking.k8s.io/v1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{
				"kind":       "GatewayPolicy",
				"apiVersion": "consul.hashicorp.com/v1alpha1",
				"metadata": map[string]interface{}{
					"name":      "my-policy",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"targetRef": map[string]interface{}{
						"group": tc.inputGroup,
						"kind":  "Gateway",
						"name":  "my-gateway",
					},
				},
			}
			convertGatewayPolicyTargetRef(raw)
			spec := raw["spec"].(map[string]interface{})
			targetRef := spec["targetRef"].(map[string]interface{})
			// targetRef.group must be rewritten (or left alone) as expected
			require.Equal(t, tc.wantTargetRef, targetRef["group"])
			// the GatewayPolicy's own apiVersion must never be touched
			require.Equal(t, "consul.hashicorp.com/v1alpha1", raw["apiVersion"])
		})
	}
}

func TestConvertToConsulGatewayPolicy(t *testing.T) {
	// targetRef.group is rewritten regardless of whether it is v1beta1 or v1; apiVersion is left untouched.
	cases := []struct {
		name          string
		inputGroup    string
		wantTargetRef string
	}{
		{
			name:          "v1beta1 group rewritten to consul",
			inputGroup:    "gateway.networking.k8s.io/v1beta1",
			wantTargetRef: "consul.hashicorp.com/v1beta1",
		},
		{
			name:          "v1 group (already rewritten) rewritten to consul",
			inputGroup:    "gateway.networking.k8s.io/v1",
			wantTargetRef: "consul.hashicorp.com/v1beta1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{
				"kind":       "GatewayPolicy",
				"apiVersion": "consul.hashicorp.com/v1alpha1",
				"metadata": map[string]interface{}{
					"name":      "my-policy",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"targetRef": map[string]interface{}{
						"group": tc.inputGroup,
						"kind":  "Gateway",
						"name":  "my-gateway",
					},
				},
			}
			convertToConsulGatewayPolicy(raw)
			// apiVersion must remain unchanged
			require.Equal(t, "consul.hashicorp.com/v1alpha1", raw["apiVersion"])
			spec := raw["spec"].(map[string]interface{})
			targetRef := spec["targetRef"].(map[string]interface{})
			// targetRef.group must be rewritten to consul.hashicorp.com/v1beta1
			require.Equal(t, tc.wantTargetRef, targetRef["group"])
		})
	}
}

func TestConvertGatewayPolicyTargetRef_Guards(t *testing.T) {
	t.Run("nil spec — no panic", func(t *testing.T) {
		raw := map[string]interface{}{
			"kind":       "GatewayPolicy",
			"apiVersion": "consul.hashicorp.com/v1alpha1",
		}
		// must not panic
		convertGatewayPolicyTargetRef(raw)
		require.Equal(t, "consul.hashicorp.com/v1alpha1", raw["apiVersion"])
	})

	t.Run("missing targetRef — no panic", func(t *testing.T) {
		raw := map[string]interface{}{
			"kind":       "GatewayPolicy",
			"apiVersion": "consul.hashicorp.com/v1alpha1",
			"spec":       map[string]interface{}{},
		}
		// must not panic
		convertGatewayPolicyTargetRef(raw)
		require.Equal(t, "consul.hashicorp.com/v1alpha1", raw["apiVersion"])
	})
}

func TestConvertToConsulGatewayPolicy_Guards(t *testing.T) {
	t.Run("nil spec — no panic", func(t *testing.T) {
		raw := map[string]interface{}{
			"kind":       "GatewayPolicy",
			"apiVersion": "consul.hashicorp.com/v1alpha1",
		}
		// must not panic
		convertToConsulGatewayPolicy(raw)
		require.Equal(t, "consul.hashicorp.com/v1alpha1", raw["apiVersion"])
	})

	t.Run("missing targetRef — no panic", func(t *testing.T) {
		raw := map[string]interface{}{
			"kind":       "GatewayPolicy",
			"apiVersion": "consul.hashicorp.com/v1alpha1",
			"spec":       map[string]interface{}{},
		}
		// must not panic
		convertToConsulGatewayPolicy(raw)
		require.Equal(t, "consul.hashicorp.com/v1alpha1", raw["apiVersion"])
	})
}

