// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestOpenShiftStaticServerOverlay(t *testing.T) {
	kubectl, err := exec.LookPath("kubectl")
	if err != nil {
		t.Skip("kubectl is required to validate local kustomize rendering")
	}
	for _, fixture := range []string{"kitchen-sink", "kitchen-sink-ent", "ext-authz-oidc-mesh"} {
		for _, cni := range []bool{false, true} {
			name := fixture
			if cni {
				name += "-cni"
			}
			t.Run(name, func(t *testing.T) {
				cfg := &config.TestConfig{EnableOpenshift: true, EnableCNI: cni}
				directory := OpenShiftStaticServerOverlay(t, cfg, "../../tests/fixtures/cases/api-gateways/"+fixture, "consul")
				output, err := exec.Command(kubectl, "kustomize", directory).CombinedOutput()
				require.NoError(t, err, string(output))
				decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(output)), 4096)
				serverFound := false
				for {
					var deployment appsv1.Deployment
					err := decoder.Decode(&deployment)
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					if deployment.Kind != "Deployment" || deployment.Name != "static-server" {
						continue
					}
					serverFound = true
					require.Equal(t, "consul", deployment.Namespace)
					if cni {
						require.Contains(t, deployment.Spec.Template.Annotations, "k8s.v1.cni.cncf.io/networks")
					} else {
						require.NotContains(t, deployment.Spec.Template.Annotations, "k8s.v1.cni.cncf.io/networks")
					}
					require.Contains(t, deployment.Spec.Template.Spec.Containers[0].Image, "kschoche/http-echo")
				}
				require.True(t, serverFound, "the composite fixture must retain its backend Deployment")
			})
		}
	}
}

func TestOpenShiftStaticServerOverlayNonOpenShift(t *testing.T) {
	require.Equal(t, "fixture", OpenShiftStaticServerOverlay(t, &config.TestConfig{}, "fixture", "default"))
}

func TestOpenShiftStaticServerHTTPSFixture(t *testing.T) {
	const basePath = "../../tests/fixtures/bases/static-server-https"
	require.Equal(t, basePath, OpenShiftStaticServerHTTPSFixture(t, &config.TestConfig{}, basePath, "consul"))

	original, err := os.ReadFile(basePath + "/configmap.yaml")
	require.NoError(t, err)
	directory := OpenShiftStaticServerHTTPSFixture(t, &config.TestConfig{UseOpenshift: true}, basePath, "consul")
	require.NotEqual(t, basePath, directory)
	updated, err := os.ReadFile(directory + "/configmap.yaml")
	require.NoError(t, err)
	require.Contains(t, string(updated), "static-server.consul {")
	require.NotContains(t, string(updated), "static-server.default")
	require.Equal(t, strings.Replace(string(original), "static-server.default {", "static-server.consul {", 1), string(updated))
	afterRun, err := os.ReadFile(basePath + "/configmap.yaml")
	require.NoError(t, err)
	require.Equal(t, string(original), string(afterRun), "the shared fixture must not be modified")

	kubectl, err := exec.LookPath("kubectl")
	if err != nil {
		t.Skip("kubectl is required to validate local kustomize rendering")
	}
	output, err := exec.Command(kubectl, "kustomize", directory).CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "static-server.consul {")
}

func TestOpenShiftBasicReferenceGrantVersion(t *testing.T) {
	content, err := os.ReadFile("../../tests/fixtures/cases/openshift/basic/route.yaml")
	require.NoError(t, err)
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(content)), 4096)
	versions := map[string]string{}
	for {
		var resource struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
		}
		err := decoder.Decode(&resource)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		versions[resource.Kind] = resource.APIVersion
	}
	require.Equal(t, "gateway.networking.k8s.io/v1", versions["HTTPRoute"])
	require.Equal(t, "gateway.networking.k8s.io/v1beta1", versions["ReferenceGrant"])
}

func TestOpenShiftBasicDefaultResourcesAreLabeled(t *testing.T) {
	content, err := os.ReadFile("../../tests/fixtures/cases/openshift/basic/intentions.yaml")
	require.NoError(t, err)
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(content)), 4096)
	count := 0
	for {
		var resource struct {
			Metadata struct {
				Namespace string            `json:"namespace"`
				Labels    map[string]string `json:"labels"`
			} `json:"metadata"`
		}
		err := decoder.Decode(&resource)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.Equal(t, "default", resource.Metadata.Namespace)
		require.Equal(t, "consul", resource.Metadata.Labels["consul.hashicorp.com/acceptance-namespace"])
		count++
	}
	require.Equal(t, 2, count)
}
