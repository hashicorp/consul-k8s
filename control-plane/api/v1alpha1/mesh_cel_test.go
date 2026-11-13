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

func meshObject(spec map[string]interface{}, finalizers ...interface{}) map[string]interface{} {
	meta := map[string]interface{}{"name": "mesh"}
	if len(finalizers) > 0 {
		meta["finalizers"] = finalizers
	}
	return map[string]interface{}{
		"apiVersion": "consul.hashicorp.com/v1alpha1",
		"kind":       "Mesh",
		"metadata":   meta,
		"spec":       spec,
	}
}

func validateMeshCEL(t *testing.T, path string, tls map[string]interface{}) field.ErrorList {
	t.Helper()
	validator, structural := meshCELValidator(t, path)
	errs, _ := validator.Validate(context.Background(), field.NewPath("root"), structural, meshObject(map[string]interface{}{"tls": tls}), nil, celconfig.RuntimeCELCostBudget)
	return errs
}

// validateMeshCELUpdate evaluates the CRD rules for an update from oldObj to
// obj without validation ratcheting, as on Kubernetes versions before 1.30.
func validateMeshCELUpdate(t *testing.T, path string, oldObj, obj map[string]interface{}) field.ErrorList {
	t.Helper()
	validator, structural := meshCELValidator(t, path)
	errs, _ := validator.Validate(context.Background(), field.NewPath("root"), structural, obj, oldObj, celconfig.RuntimeCELCostBudget)
	return errs
}

func TestMeshCRD_CEL_ECDHCurves(t *testing.T) {
	const minVersionMsg = "ecdhCurves can only be configured when tlsMinVersion is 'TLSv1_2' or higher"
	// Include P-256 so these cases isolate the tlsMinVersion rule from the
	// outgoing P-256 rule covered by TestMeshCRD_CEL_OutgoingECDHCurvesRequireP256.
	curves := []interface{}{"X25519MLKEM768", "X25519", "P-256"}

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

func TestMeshCRD_CEL_OutgoingECDHCurvesRequireP256(t *testing.T) {
	const p256Msg = `spec.tls.outgoing.ecdhCurves must include "P-256" when TLS 1.2 can be negotiated`
	pqc := []interface{}{"X25519MLKEM768", "X25519"}
	outgoing := func(min string, curves ...interface{}) map[string]interface{} {
		o := map[string]interface{}{"ecdhCurves": curves}
		if min != "" {
			o["tlsMinVersion"] = min
		}
		return map[string]interface{}{"tls": map[string]interface{}{"outgoing": o}}
	}
	incoming := func(min string, curves ...interface{}) map[string]interface{} {
		return map[string]interface{}{"tls": map[string]interface{}{"incoming": map[string]interface{}{"ecdhCurves": curves, "tlsMinVersion": min}}}
	}
	withTProxy := func(spec map[string]interface{}) map[string]interface{} {
		spec["transparentProxy"] = map[string]interface{}{"meshDestinationsOnly": true}
		return spec
	}

	cases := map[string]struct {
		old     map[string]interface{} // nil means create
		new     map[string]interface{}
		wantErr string
	}{
		// Rules that compare with the old object are not evaluated on create,
		// so the webhook enforces the requirement for new resources.
		"create with outgoing curves lacking P-256 is left to the webhook": {
			new: meshObject(outgoing("TLSv1_2", pqc...)),
		},

		// Without ratcheting, every update is validated against the whole
		// object. An unchanged outgoing block must not block updates such as
		// adding a finalizer or changing other fields.
		"update adding a finalizer keeps unchanged incompatible outgoing curves": {
			old: meshObject(outgoing("TLSv1_2", pqc...)),
			new: meshObject(outgoing("TLSv1_2", pqc...), "finalizers.consul.hashicorp.com"),
		},
		"update changing another field keeps unchanged incompatible outgoing curves": {
			old: meshObject(outgoing("", pqc...)),
			new: meshObject(withTProxy(outgoing("", pqc...))),
		},
		"update changing outgoing curves to another incompatible list is rejected": {
			old:     meshObject(outgoing("TLSv1_2", pqc...)),
			new:     meshObject(outgoing("TLSv1_2", "X25519")),
			wantErr: p256Msg,
		},
		"update lowering outgoing tlsMinVersion from TLSv1_3 is rejected": {
			old:     meshObject(outgoing("TLSv1_3", pqc...)),
			new:     meshObject(outgoing("TLSv1_2", pqc...)),
			wantErr: p256Msg,
		},
		"update removing outgoing tlsMinVersion TLSv1_3 is rejected": {
			old:     meshObject(outgoing("TLSv1_3", pqc...)),
			new:     meshObject(outgoing("", pqc...)),
			wantErr: p256Msg,
		},
		"update adding incompatible outgoing curves to a resource without TLS is rejected": {
			old:     meshObject(map[string]interface{}{}),
			new:     meshObject(outgoing("TLS_AUTO", pqc...)),
			wantErr: p256Msg,
		},
		"update adding P-256 to incompatible outgoing curves is accepted": {
			old: meshObject(outgoing("TLSv1_2", pqc...)),
			new: meshObject(outgoing("TLSv1_2", "X25519MLKEM768", "X25519", "P-256")),
		},
		"update setting outgoing tlsMinVersion to TLSv1_3 is accepted": {
			old: meshObject(outgoing("TLSv1_2", pqc...)),
			new: meshObject(outgoing("TLSv1_3", pqc...)),
		},
		"update emptying outgoing curves is accepted": {
			old: meshObject(outgoing("TLSv1_2", pqc...)),
			new: meshObject(outgoing("TLSv1_2")),
		},
		"update changing incoming curves lacking P-256 is accepted": {
			old: meshObject(incoming("TLSv1_2", pqc...)),
			new: meshObject(incoming("TLSv1_2", "X25519")),
		},
	}
	for crdName, path := range meshCRDFiles {
		for name, tc := range cases {
			t.Run(crdName+"/"+name, func(t *testing.T) {
				var errs field.ErrorList
				if tc.old == nil {
					validator, structural := meshCELValidator(t, path)
					errs, _ = validator.Validate(context.Background(), field.NewPath("root"), structural, tc.new, nil, celconfig.RuntimeCELCostBudget)
				} else {
					errs = validateMeshCELUpdate(t, path, tc.old, tc.new)
				}
				if tc.wantErr == "" {
					require.Empty(t, errs)
					return
				}
				require.Len(t, errs, 1)
				require.Contains(t, errs[0].Error(), tc.wantErr)
				require.Contains(t, errs[0].Field, "root.spec")
			})
		}
	}
}
