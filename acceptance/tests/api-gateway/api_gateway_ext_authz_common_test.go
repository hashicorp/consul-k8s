// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// configureLocalExtAuthzDev wires an ext_authz api-gateway test up to
// locally-built images and a license file when EXT_AUTHZ_LOCAL_DEV is set,
// mirroring the defaults used by hack/api-gw-ext-authz-kind/scripts/lib.sh. This
// lets a developer run the tests against `make dev-docker` / locally-built
// consul-enterprise images loaded into a kind cluster without threading a long
// list of flags. When the toggle is off the function is a no-op and the standard
// flag-driven behaviour applies.
//
// Overridable environment variables (with hack-matching defaults):
//   - CONSUL_IMAGE          (hashicorp/consul-enterprise:local)
//   - CONTROL_PLANE_IMAGE   (consul-k8s-control-plane:local)
//   - DATAPLANE_IMAGE       (consul-dataplane:1.10.0-dev)
//   - CONSUL_LICENSE_PATH   (/Users/bharath/hashicorp/consul-enterprise/consul.hclic)
func configureLocalExtAuthzDev(t *testing.T, cfg *config.TestConfig, helmValues map[string]string) {
	t.Helper()

	enabled, _ := strconv.ParseBool(os.Getenv("EXT_AUTHZ_LOCAL_DEV"))
	if !enabled {
		return
	}

	consulImage := envOrDefault("CONSUL_IMAGE", "hashicorp/consul-enterprise:local")
	controlPlaneImage := envOrDefault("CONTROL_PLANE_IMAGE", "consul-k8s-control-plane:local")
	dataplaneImage := envOrDefault("DATAPLANE_IMAGE", "consul-dataplane:1.10.0-dev")

	// helmValues are merged last by NewHelmCluster, so these win over any
	// flag-derived image values.
	helmValues["global.image"] = consulImage
	helmValues["global.imageK8S"] = controlPlaneImage
	helmValues["global.imageConsulDataplane"] = dataplaneImage
	// Locally-loaded images use stable tags and are never pushed to a registry.
	helmValues["global.imagePullPolicy"] = "IfNotPresent"

	// ext_authz for api-gateway is Enterprise-only. Load the license from a file
	// so the framework creates the license secret and the enterprise skip gate
	// passes, matching how the hack scripts create the secret from consul.hclic.
	cfg.EnableEnterprise = true
	if cfg.EnterpriseLicense == "" {
		licensePath := envOrDefault("CONSUL_LICENSE_PATH", "/Users/bharath/hashicorp/consul-enterprise/consul.hclic")
		license, err := os.ReadFile(licensePath)
		require.NoErrorf(t, err, "EXT_AUTHZ_LOCAL_DEV: failed to read Consul Enterprise license from %s", licensePath)
		cfg.EnterpriseLicense = strings.TrimSpace(string(license))
	}

	logger.Logf(t, "EXT_AUTHZ_LOCAL_DEV enabled: consul=%s control-plane=%s dataplane=%s",
		consulImage, controlPlaneImage, dataplaneImage)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
