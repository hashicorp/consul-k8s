// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"sigs.k8s.io/yaml"
)

// meshCRDFiles are the generated Mesh CRDs. The Helm chart copy is what users
// install, the bases copy is the controller-gen output. Both must enforce the
// same CEL rules.
var meshCRDFiles = map[string]string{
	"chart": filepath.Join("..", "..", "..", "charts", "consul", "templates", "crd-meshes.yaml"),
	"bases": filepath.Join("..", "..", "config", "crd", "bases", "consul.hashicorp.com_meshes.yaml"),
}

// helmDirective matches Helm template lines such as `{{- if ... }}` and
// `app: {{ template "consul.name" . }}` so the chart CRD can be parsed as YAML.
var helmDirective = regexp.MustCompile(`(?m)^.*\{\{.*\}\}.*$`)

func loadMeshCRDSchema(t *testing.T, path string) *apiextensionsv1.JSONSchemaProps {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	raw = helmDirective.ReplaceAll(raw, nil)

	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(raw, &crd))
	require.Equal(t, "meshes.consul.hashicorp.com", crd.Name)
	require.Len(t, crd.Spec.Versions, 1)
	return crd.Spec.Versions[0].Schema.OpenAPIV3Schema
}

func meshCELValidator(t *testing.T, path string) (*cel.Validator, *schema.Structural) {
	t.Helper()
	v1Schema := loadMeshCRDSchema(t, path)

	var internal apiextensions.JSONSchemaProps
	require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(v1Schema, &internal, nil))
	structural, err := schema.NewStructural(&internal)
	require.NoError(t, err)

	validator := cel.NewValidator(structural, true, celconfig.PerCallLimit)
	require.NotNil(t, validator, "expected the Mesh CRD to contain CEL validation rules")
	return validator, structural
}

func validateMeshCEL(t *testing.T, path string, tls map[string]interface{}) field.ErrorList {
	t.Helper()
	validator, structural := meshCELValidator(t, path)
	obj := map[string]interface{}{
		"apiVersion": "consul.hashicorp.com/v1alpha1",
		"kind":       "Mesh",
		"metadata":   map[string]interface{}{"name": "mesh"},
		"spec":       map[string]interface{}{"tls": tls},
	}
	errs, _ := validator.Validate(context.Background(), field.NewPath("root"), structural, obj, nil, celconfig.RuntimeCELCostBudget)
	return errs
}

func TestMeshCRD_CEL_ECDHCurves(t *testing.T) {
	const minVersionMsg = "ecdhCurves can only be configured when tlsMinVersion is 'TLSv1_2' or higher"
	curves := []interface{}{"X25519MLKEM768", "X25519"}

	cases := map[string]struct {
		cfg     map[string]interface{}
		wantErr string
	}{
		"empty config": {
			cfg: map[string]interface{}{},
		},
		"curves without tlsMinVersion": {
			cfg: map[string]interface{}{"ecdhCurves": curves},
		},
		"curves with tlsMaxVersion but without tlsMinVersion": {
			cfg: map[string]interface{}{"ecdhCurves": curves, "tlsMaxVersion": "TLSv1_3"},
		},
		"curves with TLS_AUTO": {
			cfg: map[string]interface{}{"ecdhCurves": curves, "tlsMinVersion": "TLS_AUTO"},
		},
		"curves with TLSv1_2": {
			cfg: map[string]interface{}{"ecdhCurves": curves, "tlsMinVersion": "TLSv1_2"},
		},
		"curves with TLSv1_3": {
			cfg: map[string]interface{}{"ecdhCurves": curves, "tlsMinVersion": "TLSv1_3"},
		},
		"curves with TLSv1_0": {
			cfg:     map[string]interface{}{"ecdhCurves": curves, "tlsMinVersion": "TLSv1_0"},
			wantErr: minVersionMsg,
		},
		"curves with TLSv1_1": {
			cfg:     map[string]interface{}{"ecdhCurves": curves, "tlsMinVersion": "TLSv1_1"},
			wantErr: minVersionMsg,
		},
		"empty curves with TLSv1_0": {
			cfg: map[string]interface{}{"ecdhCurves": []interface{}{}, "tlsMinVersion": "TLSv1_0"},
		},
		"TLSv1_0 without curves": {
			cfg: map[string]interface{}{"tlsMinVersion": "TLSv1_0"},
		},
	}

	for crdName, path := range meshCRDFiles {
		for _, direction := range []string{"incoming", "outgoing"} {
			for name, tc := range cases {
				t.Run(crdName+"/"+direction+"/"+name, func(t *testing.T) {
					errs := validateMeshCEL(t, path, map[string]interface{}{direction: tc.cfg})
					if tc.wantErr == "" {
						require.Empty(t, errs)
						return
					}
					require.Len(t, errs, 1)
					require.Contains(t, errs[0].Error(), tc.wantErr)
					require.Contains(t, errs[0].Field, "spec.tls."+direction)
				})
			}
		}
	}
}

func TestMeshCRD_ChartMatchesBases(t *testing.T) {
	chart := loadMeshCRDSchema(t, meshCRDFiles["chart"])
	bases := loadMeshCRDSchema(t, meshCRDFiles["bases"])
	require.Equal(t, bases, chart, "regenerate the Mesh CRD so the Helm chart and config/crd/bases copies match")
}
