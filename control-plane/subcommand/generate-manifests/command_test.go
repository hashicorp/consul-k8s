// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package generatemanifests

import (
	"encoding/json"
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
		"no client": {
			client: nil,
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

		{"EmptyKind", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{"kind": tc.kind}
			enforceGatewayAPIVersion(raw)
			got, _ := raw["apiVersion"].(string)
			require.Equal(t, tc.wantGroup, got)
		})
	}
}

func TestEnforceConsulAPIVersion(t *testing.T) {
	cases := []struct {
		name      string
		kind      string
		APIGroup  string
		wantGroup string
	}{

		{"Gateway", "Gateway", "gateway.networking.k8s.io/v1beta1", "consul.hashicorp.com/v1beta1"},
		{"HTTPRoute", "HTTPRoute", "gateway.networking.k8s.io/v1beta1", "consul.hashicorp.com/v1beta1"},
		{"GRPCRoute", "GRPCRoute", "gateway.networking.k8s.io/v1beta1", "consul.hashicorp.com/v1beta1"},
		{"ReferenceGrant", "ReferenceGrant", "gateway.networking.k8s.io/v1beta1", "consul.hashicorp.com/v1beta1"},
		{"UDPRoute", "UDPRoute", "gateway.networking.k8s.io/v1alpha2", "consul.hashicorp.com/v1alpha2"},
		{"TLSRoute", "TLSRoute", "gateway.networking.k8s.io/v1alpha2", "consul.hashicorp.com/v1alpha2"},
		{"TCPRoute", "TCPRoute", "gateway.networking.k8s.io/v1alpha2", "consul.hashicorp.com/v1alpha2"},

		{"EmptyKind", "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{"kind": tc.kind}
			enforceConsulApiVersion(raw)
			got, _ := raw["apiVersion"].(string)
			require.Equal(t, tc.wantGroup, got)
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}

func TestEnforceConsulApiVersion(t *testing.T) {
	// gw
	gw := gwv1.Gateway{}
	gw.APIVersion = "gateway.networking.k8s.io/v1beta1"
	gw.Kind = "Gateway"
	gw.ObjectMeta.Name = "api-gateway"
	gw.Spec.GatewayClassName = "test"
	from := gwv1.NamespacesFromAll
	gw.Spec.Listeners = []gwv1.Listener{
		{
			Name:     "http",
			Port:     80,
			Protocol: gwv1.HTTPProtocolType, // better than string
			AllowedRoutes: &gwv1.AllowedRoutes{
				Namespaces: &gwv1.RouteNamespaces{
					From: &from,
				},
			},
		},
	}

	// httproute
	httproute := gwv1.HTTPRoute{}
	httproute.APIVersion = "gateway.networking.k8s.io/v1beta1"
	httproute.Kind = "HTTPRoute"
	httproute.ObjectMeta.Name = "http-route"
	gwKind := gwv1.Kind("Gateway")
	group := gwv1.Group("gateway.networking.k8s.io")

	httproute.Spec.ParentRefs = []gwv1.ParentReference{
		{
			Group: &group,
			Kind:  &gwKind,
			Name:  "api-gateway",
		},
	}
	ruleType := gwv1.PathMatchExact
	httproute.Spec.Rules = []gwv1.HTTPRouteRule{
		{
			Matches: []gwv1.HTTPRouteMatch{
				{
					Path: &gwv1.HTTPPathMatch{
						Type:  &ruleType,
						Value: ptr("/api"),
					},
				},
			},
			BackendRefs: []gwv1.HTTPBackendRef{
				{
					BackendRef: gwv1.BackendRef{
						BackendObjectReference: gwv1.BackendObjectReference{
							Kind:      ptr(gwv1.Kind("Service")),
							Name:      gwv1.ObjectName("public-api"),
							Namespace: ptr(gwv1.Namespace("default")),
							Port:      ptr(gwv1.PortNumber(8080)),
						},
					},
				},
			},
		},
	}

	// grpcroute
	grpcRoute := gwv1.GRPCRoute{}
	grpcRoute.APIVersion = "gateway.networking.k8s.io/v1beta1"
	grpcRoute.Kind = "GRPCRoute"
	grpcRoute.ObjectMeta.Name = "grpc-route"
	grpcRoute.Spec.ParentRefs = []gwv1.ParentReference{
		{
			Group: &group,
			Kind:  &gwKind,
			Name:  "api-gateway",
		},
	}

	// tcproute
	tcpRoute := gwv1alpha2.TCPRoute{}
	tcpRoute.APIVersion = "gateway.networking.k8s.io/v1alpha2"
	tcpRoute.Kind = "TCPRoute"
	tcpRoute.ObjectMeta.Name = "tcp-route"
	tcpRoute.Spec.ParentRefs = []gwv1alpha2.ParentReference{
		{
			Group: &group,
			Kind:  &gwKind,
			Name:  "api-gateway",
		},
	}
	// reference grant
	rg := gwv1beta1.ReferenceGrant{}
	rg.APIVersion = "gateway.networking.k8s.io/v1beta1"
	rg.Kind = "ReferenceGrant"
	rg.ObjectMeta.Name = "reference-grant"
	rg.ObjectMeta.Namespace = "default"
	rg.Spec.From = []gwv1beta1.ReferenceGrantFrom{
		{
			Group:     "gateway.networking.k8s.io",
			Kind:      "HttpRoute",
			Namespace: "consul",
		}}
	rg.Spec.To = []gwv1beta1.ReferenceGrantTo{
		{
			Group: "",
			Kind:  "Service",
		},
	}

	cases := []struct {
		name     string
		input    any
		validate func(t *testing.T, raw map[string]interface{})
	}{
		{
			name:  "Gateway",
			input: &gw,
			validate: func(t *testing.T, raw map[string]interface{}) {
				require.Equal(t, "consul.hashicorp.com/v1beta1", raw["apiVersion"])

				md := raw["metadata"].(map[string]interface{})
				require.Equal(t, "api-gateway-custom", md["name"])

				spec := raw["spec"].(map[string]interface{})
				require.Equal(t, "consul-custom", spec["gatewayClassName"])
			},
		},
		{

			name:  "HTTPRoute",
			input: &httproute,
			validate: func(t *testing.T, raw map[string]interface{}) {
				require.Equal(t, "consul.hashicorp.com/v1beta1", raw["apiVersion"])

				// metadata validation
				md := raw["metadata"].(map[string]interface{})
				require.Equal(t, "http-route-custom", md["name"])

				// parentRefs validation
				spec := raw["spec"].(map[string]interface{})
				parentRefs := spec["parentRefs"].([]interface{})
				require.Len(t, parentRefs, 1)

				pr := parentRefs[0].(map[string]interface{})
				require.Equal(t, "consul.hashicorp.com", pr["group"])
				require.Equal(t, "api-gateway-custom", pr["name"])

				// rules validation
				rules := spec["rules"].([]interface{})
				require.Len(t, rules, 1)

				rule := rules[0].(map[string]interface{})

				// matches validation
				matches := rule["matches"].([]interface{})
				require.Len(t, matches, 1)

				match := matches[0].(map[string]interface{})
				path := match["path"].(map[string]interface{})

				require.Equal(t, "Exact", path["type"])
				require.Equal(t, "/api", path["value"])

				// backendRefs validation
				backendRefs := rule["backendRefs"].([]interface{})
				require.Len(t, backendRefs, 1)

				br := backendRefs[0].(map[string]interface{})

				require.Equal(t, "Service", br["kind"])
				require.Equal(t, "public-api", br["name"])
				require.Equal(t, "default", br["namespace"])
				require.EqualValues(t, 8080, br["port"])
			},
		},
		{
			name:  "GRPCRoute",
			input: &grpcRoute,
			validate: func(t *testing.T, raw map[string]interface{}) {
				require.Equal(t, "consul.hashicorp.com/v1beta1", raw["apiVersion"])

				md := raw["metadata"].(map[string]interface{})
				require.Equal(t, "grpc-route-custom", md["name"])

				spec := raw["spec"].(map[string]interface{})
				parentRefs := spec["parentRefs"].([]interface{})
				require.Len(t, parentRefs, 1)
				pr := parentRefs[0].(map[string]interface{})
				require.Equal(t, "consul.hashicorp.com", pr["group"])
				require.Equal(t, "api-gateway-custom", pr["name"])
			},
		},
		{
			name:  "TCPRoute",
			input: &tcpRoute,
			validate: func(t *testing.T, raw map[string]interface{}) {
				require.Equal(t, "consul.hashicorp.com/v1alpha2", raw["apiVersion"])

				md := raw["metadata"].(map[string]interface{})
				require.Equal(t, "tcp-route-custom", md["name"])

				spec := raw["spec"].(map[string]interface{})
				parentRefs := spec["parentRefs"].([]interface{})
				require.Len(t, parentRefs, 1)
				pr := parentRefs[0].(map[string]interface{})
				require.Equal(t, "consul.hashicorp.com", pr["group"])
				require.Equal(t, "api-gateway-custom", pr["name"])
			},
		},
		{
			name:  "ReferenceGrant",
			input: &rg,
			validate: func(t *testing.T, raw map[string]interface{}) {
				require.Equal(t, "consul.hashicorp.com/v1beta1", raw["apiVersion"])

				md := raw["metadata"].(map[string]interface{})
				require.Equal(t, "reference-grant-custom", md["name"])
				require.Equal(t, "default", md["namespace"])

				spec := raw["spec"].(map[string]interface{})
				from := spec["from"].([]interface{})
				require.Len(t, from, 1)
				f := from[0].(map[string]interface{})
				require.Equal(t, "consul.hashicorp.com", f["group"])
				require.Equal(t, "HttpRoute", f["kind"])
				require.Equal(t, "consul", f["namespace"])

				to := spec["to"].([]interface{})
				require.Len(t, to, 1)
				toservice := to[0].(map[string]interface{})
				require.Equal(t, "", toservice["group"])
				require.Equal(t, "Service", toservice["kind"])
			},
		},
	}

	logJSON := func(t *testing.T, label string, obj interface{}) {
		b, _ := json.MarshalIndent(obj, "", "  ")
		t.Logf("%s:\n%s", label, string(b))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tc.input)
			require.NoError(t, err)

			// ✅ Ensure kind is present
			if _, ok := raw["kind"]; !ok {
				// fallback if converter didn't set it
				switch tc.input.(type) {
				case gwv1.GatewayClass:
					raw["kind"] = "GatewayClass"
				case gwv1.Gateway:
					raw["kind"] = "Gateway"
				case gwv1.HTTPRoute:
					raw["kind"] = "HTTPRoute"
				case gwv1.GRPCRoute:
					raw["kind"] = "GRPCRoute"
				case gwv1beta1.ReferenceGrant:
					raw["kind"] = "ReferenceGrant"
				case gwv1alpha2.UDPRoute:
					raw["kind"] = "UDPRoute"
				case gwv1alpha2.TLSRoute:
					raw["kind"] = "TLSRoute"
				case gwv1alpha2.TCPRoute:
					raw["kind"] = "TCPRoute"
				default:
					require.Fail(t, "unexpected type for test case input")
				}

			}

			logJSON(t, "Before", raw)

			require.NotPanics(t, func() {
				enforceConsulApiVersion(raw)
			})

			logJSON(t, "After", raw)

			tc.validate(t, raw)
		})
	}
}

// test function for writeObjects to validate the output manifests are in the expected format and have the expected mutations
func TestWriteObjects(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := &Command{
		flagManifestsGatewayAPIDir: filepath.Join(tmpDir, "gateway"),
		flagManifestsConsulAPIDir:  filepath.Join(tmpDir, "consul"),
		consulApiEnabled:           true,
		UI:                         &cli.MockUi{},
	}

	// ✅ test object
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

	// ✅ check gateway file
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

	// ✅ validate gateway mutation
	require.Equal(t, "gateway.networking.k8s.io/v1", gatewayObj["apiVersion"])

	// ✅ check consul file
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
	require.Equal(t, "api-gateway-custom", md["name"])
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
	require.Equal(t, "consul-custom", spec["gatewayClassName"])
}

// E2E
// func TestCommand_Run_E2E_WithDump(t *testing.T) {
// 	tmpDir := t.TempDir()
// 	ui := &cli.MockUi{}

// 	// Create test objects
// 	gw := &gwv1.Gateway{
// 		TypeMeta: metav1.TypeMeta{
// 			Kind:       "Gateway",
// 			APIVersion: "gateway.networking.k8s.io/v1",
// 		},
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      "test-gw",
// 			Namespace: "default",
// 		},
// 	}

// 	gw.Spec.GatewayClassName = "test-class"
// 	gw.Spec.Listeners = []gwv1.Listener{
// 		{
// 			Name:     "http",
// 			Port:     80,
// 			Protocol: gwv1.HTTPProtocolType,
// 			AllowedRoutes: &gwv1.AllowedRoutes{
// 				Namespaces: &gwv1.RouteNamespaces{
// 					From: ptr(gwv1.NamespacesFromAll),
// 				},
// 			},
// 		},
// 		{
// 			Name:     "https",
// 			Port:     443,
// 			Protocol: gwv1.HTTPSProtocolType,
// 		},
// 	}
// 	objs := []client.Object{gw}
// 	// Scheme (IMPORTANT)
// 	s := runtime.NewScheme()
// 	require.NoError(t, clientgoscheme.AddToScheme(s))
// 	require.NoError(t, gwv1.Install(s))
// 	require.NoError(t, gwv1beta1.Install(s))
// 	require.NoError(t, gwv1alpha2.Install(s))
// 	require.NoError(t, v1alpha1.AddToScheme(s))

// 	// Fake client with objects
// 	fakeClient := fake.NewClientBuilder().
// 		WithScheme(s).
// 		WithObjects(objs...).
// 		Build()

// 	cmd := &Command{
// 		flagManifestsGatewayAPIDir: filepath.Join(tmpDir, "gateway"),
// 		flagManifestsConsulAPIDir:  filepath.Join(tmpDir, "consul"),
// 		consulApiEnabled:           true,
// 		UI:                         ui,
// 		k8sClient:                  fakeClient,
// 		ctx:                        context.TODO(),
// 	}

// 	cmd.init()

// 	// Run
// 	start := time.Now()
// 	code := cmd.Run([]string{
// 		"-chart", "test-chart",
// 		"-app", "test-app",
// 		"-release-name", "test-release",
// 		"-manifests-gatewayapi-dir", filepath.Join(tmpDir, "gateway"),
// 		"-manifests-consulapi-dir", filepath.Join(tmpDir, "consul"),
// 	})
// 	elapsed := time.Since(start)
// 	// t.Logf("Error Output: %s", ui.ErrorWriter.String())
// 	require.Equal(t, 0, code)

// 	// guard: ensure test doesn't hang unexpectedly
// 	require.Less(t, elapsed.Seconds(), 25.0)

// 	// Validate gateway output
// 	gatewayDir := filepath.Join(tmpDir, "gateway", "gateways")
// 	files, err := os.ReadDir(gatewayDir)
// 	require.NoError(t, err)
// 	require.Len(t, files, 1)

// 	// list the files in the gatewayDir for debugging
// 	gatewayFiles, err := os.ReadDir(gatewayDir)
// 	require.NoError(t, err, "should be able to read gatewayDir")
// 	for _, f := range gatewayFiles {
// 		t.Logf("Found file in gatewayDir: %s", f.Name())
// 	}

// 	content, _ := os.ReadFile(filepath.Join(gatewayDir, files[0].Name()))
// 	t.Logf("Content of gateway file:\n%s", string(content))
// 	var gatewayObj map[string]interface{}
// 	require.NoError(t, yaml.Unmarshal(content, &gatewayObj))

// 	require.Equal(t, "gateway.networking.k8s.io/v1", gatewayObj["apiVersion"])

// 	// Validate consul output
// 	consulDir := filepath.Join(tmpDir, "consul", "gateways")
// 	files, err = os.ReadDir(consulDir)
// 	require.NoError(t, err)
// 	require.Len(t, files, 1)

// 	content, _ = os.ReadFile(filepath.Join(consulDir, files[0].Name()))

// 	// print content for debugging
// 	t.Logf("Content of consul file:\n%s", string(content))

// 	var consulObj map[string]interface{}
// 	require.NoError(t, yaml.Unmarshal(content, &consulObj))

// 	require.Equal(t, "consul.hashicorp.com/v1beta1", consulObj["apiVersion"])

// 	md := consulObj["metadata"].(map[string]interface{})
// 	require.Equal(t, "api-gateway-custom", md["name"])

// 	spec := consulObj["spec"].(map[string]interface{})
// 	require.Equal(t, "consul-ocp", spec["gatewayClassName"])

// 	// deepCopySlice coverage (listeners)
// 	listeners := spec["listeners"].([]interface{})
// 	require.Len(t, listeners, 2)

// 	// UI validation
// 	require.Contains(t, ui.OutputWriter.String(), "Dumping Gateway API objects")

// }
