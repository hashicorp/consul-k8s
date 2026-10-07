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
		VaultCAConfigMap:       "vault-ca",
		TokenAudience:          "vault",
		TokenExpirationSeconds: ptr.To(int64(3600)),
		DrainSeconds:           ptr.To(int64(30)),
	}
}

// TestTerminatingGatewayCredentialConversion proves how credential injection maps
// to the Consul TerminatingGatewayConfigEntry: legacy resources are unchanged;
// deployment details (images, Vault settings) never reach Consul; enabling the
// sidecars or setting spec.credentialInjection writes the routing Consul needs to
// connect Envoy to the processor; and MatchesConsul both tolerates Consul's
// server-side defaults and detects an entry that lost its routing, so the
// controller restores it on reconcile.
func TestTerminatingGatewayCredentialConversion(t *testing.T) {
	injectService := func() LinkedService {
		return LinkedService{
			Name:       "svc",
			CAFile:     "/etc/ssl/certs/ca-certificates.crt",
			SNI:        "api.example.com",
			Credential: &LinkedServiceCredential{Mode: CredentialModeInject, BindingID: "svc-binding"},
		}
	}
	injectConsulService := func() capi.LinkedService {
		return capi.LinkedService{
			Name:       "svc",
			CAFile:     "/etc/ssl/certs/ca-certificates.crt",
			SNI:        "api.example.com",
			Credential: &capi.GatewayServiceCredential{Mode: "inject", BindingID: "svc-binding"},
		}
	}

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

	t.Run("enabled sidecars write default routing and per-service credentials, but no deployment details", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{injectService()},
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}

		act, ok := in.ToConsul("datacenter").(*capi.TerminatingGatewayConfigEntry)
		require.True(t, ok)
		require.Equal(t, &capi.TerminatingGatewayConfigEntry{
			Kind:                capi.TerminatingGateway,
			Name:                "name",
			Services:            []capi.LinkedService{injectConsulService()},
			CredentialInjection: &capi.GatewayCredentialInjection{UDSPath: "/consul/auth-socket/auth.sock"},
			Meta:                meta("datacenter"),
		}, act, "only non-secret routing reaches Consul; images/Vault settings do not")
	})

	t.Run("disabled sidecars write no routing", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{{Name: "svc"}},
				Deployment: TerminatingGatewayDeploymentSpec{
					CredentialInjection: &TerminatingGatewayCredentialInjection{Enabled: false},
				},
			},
		}
		act := in.ToConsul("datacenter").(*capi.TerminatingGatewayConfigEntry)
		require.Nil(t, act.CredentialInjection)
	})

	t.Run("explicit spec.credentialInjection for a Helm-deployed gateway overrides the defaults", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{injectService(), {Name: "plain", Credential: &LinkedServiceCredential{Mode: CredentialModeNone}}},
				CredentialInjection: &TerminatingGatewayCredentialRouting{
					UDSPath:        "/run/camp-auth/processor.sock",
					MessageTimeout: "5s",
				},
			},
		}
		act := in.ToConsul("datacenter").(*capi.TerminatingGatewayConfigEntry)
		require.Equal(t, &capi.GatewayCredentialInjection{UDSPath: "/run/camp-auth/processor.sock", MessageTimeout: "5s"}, act.CredentialInjection)
		require.Equal(t, &capi.GatewayServiceCredential{Mode: "none"}, act.Services[1].Credential)
	})

	t.Run("MatchesConsul accepts the entry as Consul stores it (server-defaulted MessageTimeout)", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{injectService()},
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}
		stored := injectConsulService()
		stored.Namespace = "default"
		require.True(t, in.MatchesConsul(&capi.TerminatingGatewayConfigEntry{
			Kind:                capi.TerminatingGateway,
			Name:                "name",
			Namespace:           "foobar",
			Services:            []capi.LinkedService{stored},
			CredentialInjection: &capi.GatewayCredentialInjection{UDSPath: "/consul/auth-socket/auth.sock", MessageTimeout: "250ms"},
			CreateIndex:         1,
			ModifyIndex:         2,
		}), "a non-matching result here would make the controller rewrite the entry on every reconcile")
	})

	t.Run("MatchesConsul detects an entry missing routing so reconcile restores it", func(t *testing.T) {
		in := &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "name"},
			Spec: TerminatingGatewaySpec{
				Services: []LinkedService{injectService()},
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}
		withoutGatewayRouting := &capi.TerminatingGatewayConfigEntry{
			Kind:     capi.TerminatingGateway,
			Name:     "name",
			Services: []capi.LinkedService{injectConsulService()},
		}
		require.False(t, in.MatchesConsul(withoutGatewayRouting))

		svc := injectConsulService()
		svc.Credential = nil
		withoutServiceCredential := &capi.TerminatingGatewayConfigEntry{
			Kind:                capi.TerminatingGateway,
			Name:                "name",
			Services:            []capi.LinkedService{svc},
			CredentialInjection: &capi.GatewayCredentialInjection{UDSPath: "/consul/auth-socket/auth.sock", MessageTimeout: "250ms"},
		}
		require.False(t, in.MatchesConsul(withoutServiceCredential))

		changedTimeout := &capi.TerminatingGatewayConfigEntry{
			Kind:                capi.TerminatingGateway,
			Name:                "name",
			Services:            []capi.LinkedService{injectConsulService()},
			CredentialInjection: &capi.GatewayCredentialInjection{UDSPath: "/consul/auth-socket/auth.sock", MessageTimeout: "5s"},
		}
		require.False(t, in.MatchesConsul(changedTimeout))
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
				Services:            []LinkedService{injectService()},
				CredentialInjection: &TerminatingGatewayCredentialRouting{MessageTimeout: "1s"},
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
		out.Spec.CredentialInjection.MessageTimeout = "9s"
		out.Spec.Services[0].Credential.BindingID = "mutated"

		require.True(t, in.Spec.Deployment.CredentialInjection.Enabled)
		require.Equal(t, "hashicorp/camp-auth-processor:TEST_VERSION", in.Spec.Deployment.CredentialInjection.ProcessorImage)
		require.Equal(t, int64(3600), *in.Spec.Deployment.CredentialInjection.TokenExpirationSeconds)
		require.Equal(t, "1s", in.Spec.CredentialInjection.MessageTimeout)
		require.Equal(t, "svc-binding", in.Spec.Services[0].Credential.BindingID)
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
		"drainSeconds negative is rejected for the kubernetesSecret source too": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.Source = CredentialSourceKubernetesSecret
				ci.SecretName = "camp-egress-credentials"
				ci.DrainSeconds = ptr.To(int64(-1))
				ci.VaultAgentImage = ""
				ci.VaultAgentConfigMap = ""
				ci.VaultAddress = ""
				ci.TokenAudience = ""
			}, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.drainSeconds`,
				`must not be negative`,
			},
		},
		"unsupported source is rejected": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.Source = "consul"
			}, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.source`,
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
		"drainSeconds above the processor's 300s drain-wait limit": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.DrainSeconds = ptr.To(int64(301)) }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.drainSeconds`,
				`must not exceed 300`,
			},
		},
		"drainSeconds above 300 is rejected for the kubernetesSecret source too": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.Source = CredentialSourceKubernetesSecret
				ci.SecretName = "camp-egress-credentials"
				ci.DrainSeconds = ptr.To(int64(600))
				ci.VaultAgentImage = ""
				ci.VaultAgentConfigMap = ""
				ci.VaultAddress = ""
				ci.TokenAudience = ""
			}, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.drainSeconds`,
				`must not exceed 300`,
			},
		},
		"drainSeconds at the 300s limit is valid": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.DrainSeconds = ptr.To(int64(300)) }, ptr.To(true)),
		},
		"drainSeconds zero (drain disabled) is valid": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.DrainSeconds = ptr.To(int64(0)) }, ptr.To(true)),
		},
		"IPv6 vault address is valid": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.VaultAddress = "https://[fd00::1]:8200" }, ptr.To(true)),
		},
		"forbidden wildcard in processorConfigMap": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) { ci.ProcessorConfigMap = "camp-*" }, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.processorConfigMap`,
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
		"kubernetesSecret source without secretName is rejected": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.Source = CredentialSourceKubernetesSecret
				ci.SecretName = ""
				ci.VaultAgentImage = ""
				ci.VaultAgentConfigMap = ""
				ci.VaultAddress = ""
				ci.TokenAudience = ""
			}, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.secretName`,
			},
		},
		"kubernetesSecret source does not require vault fields": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.Source = CredentialSourceKubernetesSecret
				ci.SecretName = "camp-egress-credentials"
				ci.VaultAgentImage = ""
				ci.VaultAgentConfigMap = ""
				ci.VaultAddress = ""
				ci.TokenAudience = ""
			}, ptr.To(true)),
			expectedErrMsgs: nil,
		},
		"kubernetesSecret source rejects wildcard secretName": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.Source = CredentialSourceKubernetesSecret
				ci.SecretName = "camp-*"
				ci.VaultAgentImage = ""
				ci.VaultAgentConfigMap = ""
				ci.VaultAddress = ""
				ci.TokenAudience = ""
			}, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.secretName`,
				`wildcard`,
			},
		},
		"kubernetesSecret source rejects wildcard processorConfigMap": {
			input: baseGateway(func(ci *TerminatingGatewayCredentialInjection) {
				ci.Source = CredentialSourceKubernetesSecret
				ci.SecretName = "camp-egress-credentials"
				ci.ProcessorConfigMap = "camp-*"
				ci.VaultAgentImage = ""
				ci.VaultAgentConfigMap = ""
				ci.VaultAddress = ""
				ci.TokenAudience = ""
			}, ptr.To(true)),
			expectedErrMsgs: []string{
				`spec.deployment.credentialInjection.processorConfigMap`,
				`wildcard`,
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
		"source":                 "string",
		"secretName":             "string",
		"processorImage":         "string",
		"vaultAgentImage":        "string",
		"processorConfigMap":     "string",
		"vaultAgentConfigMap":    "string",
		"vaultAddress":           "string",
		"vaultNamespace":         "string",
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

	// Routing fields that reach the Consul config entry must also be declared
	// explicitly, or the API server would prune them and silently disable injection.
	routing, ok := specProps["credentialInjection"].(map[string]interface{})
	require.True(t, ok, "spec.credentialInjection must be present in the structural schema")
	routingProps := routing["properties"].(map[string]interface{})
	for _, f := range []string{"udsPath", "messageTimeout"} {
		require.Equal(t, "string", routingProps[f].(map[string]interface{})["type"], "spec.credentialInjection.%s", f)
	}
	require.Len(t, routingProps, 2)

	svcProps := specProps["services"].(map[string]interface{})["items"].(map[string]interface{})["properties"].(map[string]interface{})
	credential, ok := svcProps["credential"].(map[string]interface{})
	require.True(t, ok, "spec.services[].credential must be present in the structural schema")
	credProps := credential["properties"].(map[string]interface{})
	require.Equal(t, "string", credProps["bindingID"].(map[string]interface{})["type"])
	mode := credProps["mode"].(map[string]interface{})
	require.Equal(t, "string", mode["type"])
	require.ElementsMatch(t, []interface{}{"inject", "none"}, mode["enum"])
}

// TestTerminatingGatewayCredentialRouting_Validate covers the routing rules that
// mirror Consul Enterprise's config-entry validation, so a resource Consul would
// reject (or one whose processor Envoy could never reach) fails at admission.
func TestTerminatingGatewayCredentialRouting_Validate(t *testing.T) {
	injectSvc := func(name, binding string) LinkedService {
		return LinkedService{
			Name:       name,
			CAFile:     "/etc/ssl/certs/ca-certificates.crt",
			SNI:        "api.example.com",
			Credential: &LinkedServiceCredential{Mode: CredentialModeInject, BindingID: binding},
		}
	}
	deployed := func(svcs ...LinkedService) *TerminatingGateway {
		return &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			Spec: TerminatingGatewaySpec{
				Services: svcs,
				Deployment: TerminatingGatewayDeploymentSpec{
					EnableDeployment:    ptr.To(true),
					CredentialInjection: validCredentialInjection(),
				},
			},
		}
	}
	helmManaged := func(r *TerminatingGatewayCredentialRouting, svcs ...LinkedService) *TerminatingGateway {
		return &TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			Spec:       TerminatingGatewaySpec{Services: svcs, CredentialInjection: r},
		}
	}
	mutate := func(s LinkedService, f func(*LinkedService)) LinkedService { f(&s); return s }

	cases := map[string]struct {
		input           *TerminatingGateway
		expectedErrMsgs []string
	}{
		"deployed gateway with inject and none services": {
			input: deployed(injectSvc("a", "a-binding"), LinkedService{Name: "b", Credential: &LinkedServiceCredential{Mode: CredentialModeNone}}),
		},
		"helm-managed gateway with explicit routing": {
			input: helmManaged(&TerminatingGatewayCredentialRouting{UDSPath: "/run/camp-auth/processor.sock", MessageTimeout: "5s"}, injectSvc("a", "a-binding")),
		},
		"credential without routing enabled": {
			input:           helmManaged(nil, injectSvc("a", "a-binding")),
			expectedErrMsgs: []string{`spec.services[0].credential`, `requires spec.credentialInjection`},
		},
		"service missing credential when routing enabled": {
			input:           deployed(injectSvc("a", "a-binding"), LinkedService{Name: "b"}),
			expectedErrMsgs: []string{`spec.services[1].credential`, `required`},
		},
		"wildcard service": {
			input:           helmManaged(&TerminatingGatewayCredentialRouting{}, LinkedService{Name: "*", Credential: &LinkedServiceCredential{Mode: CredentialModeNone}}),
			expectedErrMsgs: []string{`spec.services[0].name`, `wildcard`},
		},
		"unsupported mode": {
			input:           deployed(mutate(injectSvc("a", "a-binding"), func(s *LinkedService) { s.Credential.Mode = "passthrough" })),
			expectedErrMsgs: []string{`spec.services[0].credential.mode`},
		},
		"inject requires a valid bindingID": {
			input:           deployed(injectSvc("a", "Bad Binding")),
			expectedErrMsgs: []string{`spec.services[0].credential.bindingID`},
		},
		"inject requires a bindingID": {
			input:           deployed(injectSvc("a", "")),
			expectedErrMsgs: []string{`spec.services[0].credential.bindingID`},
		},
		"duplicate bindingID": {
			input:           deployed(injectSvc("a", "same"), injectSvc("b", "same")),
			expectedErrMsgs: []string{`spec.services[1].credential.bindingID`, `Duplicate`},
		},
		"inject requires caFile": {
			input:           deployed(mutate(injectSvc("a", "a-binding"), func(s *LinkedService) { s.CAFile = "" })),
			expectedErrMsgs: []string{`spec.services[0].caFile`},
		},
		"inject requires a DNS sni": {
			input:           deployed(mutate(injectSvc("a", "a-binding"), func(s *LinkedService) { s.SNI = "10.0.0.1" })),
			expectedErrMsgs: []string{`spec.services[0].sni`},
		},
		"none must not set bindingID": {
			input:           deployed(LinkedService{Name: "a", Credential: &LinkedServiceCredential{Mode: CredentialModeNone, BindingID: "x"}}),
			expectedErrMsgs: []string{`spec.services[0].credential.bindingID`, `empty`},
		},
		"relative udsPath": {
			input:           helmManaged(&TerminatingGatewayCredentialRouting{UDSPath: "auth.sock"}, injectSvc("a", "a-binding")),
			expectedErrMsgs: []string{`spec.credentialInjection.udsPath`, `absolute`},
		},
		"udsPath must match the mounted socket when sidecars are deployed": {
			input: func() *TerminatingGateway {
				gw := deployed(injectSvc("a", "a-binding"))
				gw.Spec.CredentialInjection = &TerminatingGatewayCredentialRouting{UDSPath: "/tmp/other.sock"}
				return gw
			}(),
			expectedErrMsgs: []string{`spec.credentialInjection.udsPath`, `/consul/auth-socket/auth.sock`},
		},
		"invalid messageTimeout": {
			input:           helmManaged(&TerminatingGatewayCredentialRouting{MessageTimeout: "0s"}, injectSvc("a", "a-binding")),
			expectedErrMsgs: []string{`spec.credentialInjection.messageTimeout`},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.input.Validate(common.ConsulMeta{})
			if len(tc.expectedErrMsgs) == 0 {
				require.NoError(t, err)
				require.NoError(t, tc.input.ValidateCredentialRouting())
				return
			}
			require.Error(t, err)
			for _, msg := range tc.expectedErrMsgs {
				require.Contains(t, err.Error(), msg)
			}
			require.Error(t, tc.input.ValidateCredentialRouting())
		})
	}
}

// TestTerminatingGatewayUsesCredentialInjection covers every field that ties a
// gateway to credential injection (and therefore to ai.enabled / consul-ai).
func TestTerminatingGatewayUsesCredentialInjection(t *testing.T) {
	cases := map[string]struct {
		spec TerminatingGatewaySpec
		want bool
	}{
		"plain gateway": {
			spec: TerminatingGatewaySpec{Services: []LinkedService{{Name: "web", CAFile: "ca.pem", SNI: "web.example.com"}}},
		},
		"deployment credential injection disabled": {
			spec: TerminatingGatewaySpec{Deployment: TerminatingGatewayDeploymentSpec{
				CredentialInjection: &TerminatingGatewayCredentialInjection{Enabled: false},
			}},
		},
		"deployment credential injection enabled": {
			spec: TerminatingGatewaySpec{Deployment: TerminatingGatewayDeploymentSpec{
				CredentialInjection: &TerminatingGatewayCredentialInjection{Enabled: true},
			}},
			want: true,
		},
		"routing only": {
			spec: TerminatingGatewaySpec{CredentialInjection: &TerminatingGatewayCredentialRouting{}},
			want: true,
		},
		"service credential only": {
			spec: TerminatingGatewaySpec{Services: []LinkedService{
				{Name: "web"},
				{Name: "openai", Credential: &LinkedServiceCredential{Mode: CredentialModeNone}},
			}},
			want: true,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			gw := &TerminatingGateway{Spec: c.spec}
			require.Equal(t, c.want, gw.UsesCredentialInjection())
		})
	}
}