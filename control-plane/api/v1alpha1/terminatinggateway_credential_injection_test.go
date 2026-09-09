// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

// validCredentialInjection returns the fixture from the Task 7 brief:
// a fully populated, valid spec.deployment.credentialInjection block.
func validCredentialInjection() *TerminatingGatewayCredentialInjection {
	return &TerminatingGatewayCredentialInjection{
		Enabled:                true,
		ProcessorImage:         "hashicorp/camp-auth-processor:TEST_VERSION",
		VaultAgentImage:        "hashicorp/vault:TEST_VERSION",
		ProcessorConfigMap:     "camp-egress-bindings",
		VaultAgentConfigMap:    "camp-egress-vault-agent",
		VaultAddress:           "https://vault.example:8200",
		VaultNamespace:         "",
		VaultAuthRole:          "camp-egress",
		VaultAuthMount:         "kubernetes",
		VaultCAConfigMap:       "vault-ca",
		TokenAudience:          "vault",
		TokenExpirationSeconds: ptr.To(int64(3600)),
		DrainSeconds:           ptr.To(int64(30)),
	}
}

// TestTerminatingGatewayCredentialConversion proves that the deployment-only
// spec.deployment.credentialInjection block does not change ToConsul/MatchesConsul
// behavior (it is a Kubernetes-side sidecar deployment concern, not part of the
// routing-level Consul TerminatingGatewayConfigEntry contract), that namespace
// defaulting continues to work unaffected, and that legacy resources with a nil
// credentialInjection block keep behaving exactly as before.
func TestTerminatingGatewayCredentialConversion(t *testing.T) {
	t.Run("nil legacy credentialInjection round trips exactly as before", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{{Name: "svc"}},
			},
		}
		require.Nil(t, in.Spec.Deployment.CredentialInjection)

		act := in.ToConsul("datacenter")
		resource, ok := act.(*capi.TerminatingGatewayConfigEntry)
		require.True(t, ok)
		require.Equal(t, &capi.TerminatingGatewayConfigEntry{
			Kind: capi.TerminatingGateway,
			Name: "name",
			Services: []capi.LinkedService{
				{Name: "svc"},
			},
			Meta: meta("datacenter"),
		}, resource)

		require.True(t, in.MatchesConsul(&capi.TerminatingGatewayConfigEntry{
			Kind: capi.TerminatingGateway,
			Name: "name",
			Services: []capi.LinkedService{
				{Name: "svc", Namespace: "default"},
			},
		}))
	})

	t.Run("populated credentialInjection does not alter ToConsul output", func(t *testing.T) {
		withCredentialInjection := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{{Name: "svc"}},
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}
		without := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{{Name: "svc"}},
			},
		}

		actWith := withCredentialInjection.ToConsul("datacenter")
		actWithout := without.ToConsul("datacenter")
		require.Equal(t, actWithout, actWith, "credentialInjection must not change the Consul config entry shape")
	})

	t.Run("MatchesConsul unaffected by credentialInjection presence", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{{Name: "svc"}},
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}
		require.True(t, in.MatchesConsul(&capi.TerminatingGatewayConfigEntry{
			Kind:      capi.TerminatingGateway,
			Name:      "name",
			Namespace: "foobar",
			Services: []capi.LinkedService{
				{Name: "svc", Namespace: "default"},
			},
			CreateIndex: 1,
			ModifyIndex: 2,
		}))
	})

	t.Run("namespace defaults still applied when credentialInjection is set", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{{Name: "foo"}},
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}
		in.DefaultNamespaceFields(common.ConsulMeta{
			NamespacesEnabled:    true,
			DestinationNamespace: "",
			Mirroring:            true,
			Prefix:               "ns-",
		})
		require.Equal(t, "ns-bar", in.Spec.Services[0].Namespace)
		require.NotNil(t, in.Spec.Deployment.CredentialInjection)
		require.True(t, in.Spec.Deployment.CredentialInjection.Enabled)
	})

	t.Run("deepcopy produces an independent credentialInjection block", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}
		out := in.DeepCopy()
		out.Spec.Deployment.CredentialInjection.Enabled = false
		out.Spec.Deployment.CredentialInjection.ProcessorImage = "mutated"
		*out.Spec.Deployment.CredentialInjection.TokenExpirationSeconds = 1

		require.True(t, in.Spec.Deployment.CredentialInjection.Enabled)
		require.Equal(t, "hashicorp/camp-auth-processor:TEST_VERSION", in.Spec.Deployment.CredentialInjection.ProcessorImage)
		require.Equal(t, int64(3600), *in.Spec.Deployment.CredentialInjection.TokenExpirationSeconds)
	})
}

// TestTerminatingGatewayCredentialInjection_Validate covers the required validation
// scenarios from the Task 7 brief: feature/deployment mismatch, missing image/config
// maps, invalid Vault URL/TLS, invalid projected-token fields, and forbidden
// wildcard/mode combinations.
func TestTerminatingGatewayCredentialInjection_Validate(t *testing.T) {
	baseGateway := func(mutate func(ci *TerminatingGatewayCredentialInjection), enableDeployment *bool) *TerminatingGateway {
		ci := validCredentialInjection()
		if mutate != nil {
			mutate(ci)
		}
		return &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			Spec: TerminatingGatewaySpec{
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    enableDeployment,
					CredentialInjection: ci,
				},
			},
		}
	}

	cases := map[string]struct {
		input           *TerminatingGateway
		expectedErrMsgs []string
	}{
		"valid configuration passes": {
			input:           baseGateway(nil, ptr.To(true)),
			expectedErrMsgs: nil,
		},
		"disabled feature requires nothing": {
			input: &TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: TerminatingGatewaySpec{
					Deployment: TerminatingGatewayDeploymentSpec{
						CredentialInjection: &TerminatingGatewayCredentialInjection{Enabled: false},
					},
				},
			},
			expectedErrMsgs: nil,
		},
		"feature/deployment mismatch: enabledDeployment false": {
			input: baseGateway(nil, ptr.To(false)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.enabled`,
				`enabledDeployment`,
			},
		},
		"feature/deployment mismatch: enabledDeployment nil": {
			input: baseGateway(nil, nil),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.enabled`,
				`enabledDeployment`,
			},
		},
		"missing processorImage": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.ProcessorImage = "" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.processorImage`,
			},
		},
		"missing vaultAgentImage": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAgentImage = "" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.vaultAgentImage`,
			},
		},
		"missing processorConfigMap": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.ProcessorConfigMap = "" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.processorConfigMap`,
			},
		},
		"missing vaultAgentConfigMap": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAgentConfigMap = "" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.vaultAgentConfigMap`,
			},
		},
		"invalid vault address: unparseable": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAddress = "://not-a-url" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.vaultAddress`,
			},
		},
		"invalid vault address: missing host": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAddress = "https://" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.vaultAddress`,
			},
		},
		"forbidden vault TLS mode: http scheme": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAddress = "http://vault.example:8200" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.vaultAddress`,
				`https`,
			},
		},
		"missing tokenAudience": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.TokenAudience = "" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.tokenAudience`,
			},
		},
		"tokenExpirationSeconds too low": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.TokenExpirationSeconds = ptr.To(int64(59)) }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.tokenExpirationSeconds`,
			},
		},
		"tokenExpirationSeconds too high": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.TokenExpirationSeconds = ptr.To(int64(100000)) }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.tokenExpirationSeconds`,
			},
		},
		"drainSeconds negative": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.DrainSeconds = ptr.To(int64(-1)) }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.drainSeconds`,
			},
		},
		"drainSeconds exceeds tokenExpirationSeconds": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.TokenExpirationSeconds = ptr.To(int64(600))
				ci.DrainSeconds = ptr.To(int64(601))
			}, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.drainSeconds`,
			},
		},
		"forbidden wildcard in vaultAuthMount": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAuthMount = "kub*" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.vaultAuthMount`,
				`wildcard`,
			},
		},
		"forbidden wildcard in tokenAudience": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.TokenAudience = "*" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.tokenAudience`,
				`wildcard`,
			},
		},
		"forbidden mode: processorConfigMap and vaultAgentConfigMap identical": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAgentConfigMap = ci.ProcessorConfigMap }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.vaultAgentConfigMap`,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.input.Validate(common.ConsulMeta{})
			if len(tc.expectedErrMsgs) == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, msg := range tc.expectedErrMsgs {
				require.Contains(t, err.Error(), msg)
			}
		})
	}
}

// TestTerminatingGatewayCredentialInjectionCRDSchema proves that the generated
// structural CRD schema declares every spec.deployment.credentialInjection field
// explicitly (so the Kubernetes API server's structural-schema pruning cannot
// silently drop them on admission) rather than falling back to an
// x-kubernetes-preserve-unknown-fields escape hatch.
func TestTerminatingGatewayCredentialInjectionCRDSchema(t *testing.T) {
	path := filepath.Join("..", "..", "config", "crd", "bases", "consul.hashicorp.com_terminatinggateways.yaml")
	b, err := os.ReadFile(path)
	require.NoError(t, err, "generated CRD file must exist; run `make ctrl-manifests`")

	var crd map[string]interface{}
	require.NoError(t, yaml.Unmarshal(b, &crd))

	versions, _ := crd["spec"].(map[string]interface{})["versions"].([]interface{})
	require.NotEmpty(t, versions)

	var v1alpha1Schema map[string]interface{}
	for _, v := range versions {
		ver := v.(map[string]interface{})
		if ver["name"] == "v1alpha1" {
			v1alpha1Schema = ver["schema"].(map[string]interface{})["openAPIV3Schema"].(map[string]interface{})
		}
	}
	require.NotNil(t, v1alpha1Schema, "v1alpha1 schema version must exist")

	specProps := v1alpha1Schema["properties"].(map[string]interface{})["spec"].(map[string]interface{})["properties"].(map[string]interface{})
	deployment, ok := specProps["deployment"].(map[string]interface{})
	require.True(t, ok, "spec.deployment must be present in the structural schema")
	require.Nil(t, deployment["x-kubernetes-preserve-unknown-fields"], "spec.deployment must not preserve unknown fields")

	deploymentProps := deployment["properties"].(map[string]interface{})
	credentialInjection, ok := deploymentProps["credentialInjection"].(map[string]interface{})
	require.True(t, ok, "spec.deployment.credentialInjection must be present in the structural schema")
	require.Equal(t, "object", credentialInjection["type"])
	require.Nil(t, credentialInjection["x-kubernetes-preserve-unknown-fields"],
		"spec.deployment.credentialInjection must declare every field explicitly and must not be pruned via preserve-unknown-fields")

	ciProps, ok := credentialInjection["properties"].(map[string]interface{})
	require.True(t, ok)

	expectedFieldTypes := map[string]string{
		"enabled":                "boolean",
		"processorImage":         "string",
		"vaultAgentImage":        "string",
		"processorConfigMap":     "string",
		"vaultAgentConfigMap":    "string",
		"vaultAddress":           "string",
		"vaultNamespace":         "string",
		"vaultAuthRole":          "string",
		"vaultAuthMount":         "string",
		"vaultCAConfigMap":       "string",
		"tokenAudience":          "string",
		"tokenExpirationSeconds": "integer",
		"drainSeconds":           "integer",
	}
	for field, expectedType := range expectedFieldTypes {
		propSchema, ok := ciProps[field].(map[string]interface{})
		require.True(t, ok, "expected structural schema property for field %q", field)
		require.Equal(t, expectedType, propSchema["type"], "unexpected type for field %q", field)
	}
	require.Len(t, ciProps, len(expectedFieldTypes), "no unexpected/undeclared credentialInjection fields")
}
