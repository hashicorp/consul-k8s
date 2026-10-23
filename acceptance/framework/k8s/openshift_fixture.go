// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/stretchr/testify/require"
)

// OpenShiftStaticServerOverlay adapts a composite fixture without editing its
// shared files or allowing kustomize to load outside its temporary root.
func OpenShiftStaticServerOverlay(t *testing.T, cfg *config.TestConfig, basePath, namespace string) string {
	t.Helper()
	if !cfg.EnableOpenshift && !cfg.UseOpenshift {
		return basePath
	}
	absoluteBase, err := filepath.Abs(basePath)
	require.NoError(t, err)
	fixturesRoot := absoluteBase
	for filepath.Base(fixturesRoot) != "fixtures" {
		parent := filepath.Dir(fixturesRoot)
		require.NotEqual(t, fixturesRoot, parent, "fixture must be within the acceptance fixtures directory")
		fixturesRoot = parent
	}
	directory := t.TempDir()
	require.NoError(t, os.CopyFS(filepath.Join(directory, "fixtures"), os.DirFS(fixturesRoot)))
	relativeBase, err := filepath.Rel(fixturesRoot, absoluteBase)
	require.NoError(t, err)
	copiedBase := filepath.Join(directory, "fixtures", relativeBase)
	kustomizationPath := filepath.Join(copiedBase, "kustomization.yaml")
	kustomization, err := os.ReadFile(kustomizationPath)
	require.NoError(t, err)
	serverFixture := "static-server-openshift"
	if cfg.EnableCNI {
		serverFixture += "-cni"
	}
	updated := strings.ReplaceAll(string(kustomization), "/static-server-inject", "/"+serverFixture)
	require.NoError(t, os.WriteFile(kustomizationPath, []byte(updated), 0600))
	overlay := fmt.Sprintf("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nnamespace: %s\nresources:\n- %s\n",
		namespace, filepath.ToSlash(filepath.Join("fixtures", relativeBase)))
	if cfg.EnableCNI {
		overlay += `patches:
- target:
    kind: Deployment
    name: oauth2-proxy
  patch: |-
    - op: add
      path: /spec/template/metadata/annotations/k8s.v1.cni.cncf.io~1networks
      value: '[{"name":"consul-cni"}]'
`
	}
	require.NoError(t, os.WriteFile(filepath.Join(directory, "kustomization.yaml"), []byte(overlay), 0600))
	return directory
}

// OpenShiftStaticServerHTTPSFixture returns a copy of the static-server-https
// fixture whose Caddy site address matches the test namespace. Caddy only
// issues its local certificate for that site name, so TLS requests to
// static-server.<namespace> fail SNI matching when OpenShift runs use a
// namespace other than default.
func OpenShiftStaticServerHTTPSFixture(t *testing.T, cfg *config.TestConfig, basePath, namespace string) string {
	t.Helper()
	if !cfg.EnableOpenshift && !cfg.UseOpenshift {
		return basePath
	}
	directory := filepath.Join(t.TempDir(), filepath.Base(basePath))
	require.NoError(t, os.CopyFS(directory, os.DirFS(basePath)))
	configMapPath := filepath.Join(directory, "configmap.yaml")
	configMap, err := os.ReadFile(configMapPath)
	require.NoError(t, err)
	const defaultSite = "static-server.default {"
	require.Contains(t, string(configMap), defaultSite, "unexpected Caddyfile in %s", configMapPath)
	updated := strings.Replace(string(configMap), defaultSite, fmt.Sprintf("static-server.%s {", namespace), 1)
	require.NoError(t, os.WriteFile(configMapPath, []byte(updated), 0600))
	return directory
}
