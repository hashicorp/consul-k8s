// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	frameworkk8s "github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	consulgwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
)

// The new Consul API Gateway is served by a second controller that watches
// Gateway API resources registered under the consul.hashicorp.com API group
// (Gateway "cgateways", HTTPRoute "chttproutes", ReferenceGrant
// "creferencegrants", CustomGatewayClass "customgatewayclasses"). It exists so
// that Consul can keep serving API Gateways on OpenShift 4.19+, where the
// gateway.networking.k8s.io CRDs are owned by the platform.
const (
	consulGatewayClassControllerName = "consul.hashicorp.com/gateway-controller-consul"
	consulGatewayClassName           = "consul-custom-class"

	// Fully-qualified kubectl resource names. The custom CRDs reuse the Kind
	// names of the upstream Gateway API, so we always address them by plural
	// to avoid kubectl resolving them to the gateway.networking.k8s.io group.
	consulGatewayResource        = "cgateways.consul.hashicorp.com"
	consulHTTPRouteResource      = "chttproutes.consul.hashicorp.com"
	consulReferenceGrantResource = "creferencegrants.consul.hashicorp.com"
)

var consulAPIGroup = consulgwv1beta1.Group(consulgwv1beta1.GroupVersion.Group)

// consulAPIGatewayHelmValues are the Helm values that enable the new Consul
// API Gateway controller and install the consul.hashicorp.com Gateway CRDs on
// OpenShift.
var consulAPIGatewayHelmValues = map[string]string{
	"global.openshift.crds.consulapi.enabled": "true",
}

// TestOpenshift_ConsulAPIGateway_Basic is the consul.hashicorp.com equivalent of
// TestOpenshift_Basic: it routes traffic Gateway --> frontend --> backend through
// a Gateway owned by the new Consul API Gateway controller in a secure installation.
func TestOpenshift_ConsulAPIGateway_Basic(t *testing.T) {
	cfg := suite.Config()
	newOpenshiftClusterWithHelmValues(t, cfg, true, false, consulAPIGatewayHelmValues)

	// this is normally called by the environment, but because we have to bypass we have to call it explicitly
	logf.SetLogger(logr.New(nil))

	ctx := suite.Environment().DefaultContext(t)
	consulClient := consulGatewayRuntimeClient(t, ctx)

	requireConsulGatewayClassAccepted(t, consulClient, consulGatewayClassName)

	logger.Log(t, "creating resources for OpenShift Consul API Gateway test")
	applyManifestDirs(t, cfg, "../fixtures/cases/openshift/basic/backend.yaml", "../fixtures/cases/openshift/basic/frontend.yaml",
		"../fixtures/cases/openshift/consul-api-gateway-basic")

	gatewayIP := waitForConsulGatewayAddress(t, consulClient, "consul-api-gateway", "consul")
	logger.Log(t, "Consul API gateway is reachable at:", gatewayIP)

	requireConsulRouteAccepted(t, consulClient, "frontend-route-consul", "consul", "consul-api-gateway", "consul")
	requireFrontendReachable(t, gatewayIP)
}

// TestOpenshift_ConsulAPIGateway_Tenancy mirrors TestOpenshift_APIGateway_Tenancy for
// the new Consul API Gateway. It verifies cross-namespace references are refused
// until consul.hashicorp.com ReferenceGrants exist, and that config entries are
// written into the correct Consul namespace with and without ACLs/TLS.
func TestOpenshift_ConsulAPIGateway_Tenancy(t *testing.T) {
	cases := []struct {
		secure             bool
		namespaceMirroring bool
	}{
		{secure: false, namespaceMirroring: false},
		{secure: true, namespaceMirroring: false},
		{secure: false, namespaceMirroring: true},
		{secure: true, namespaceMirroring: true},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t, namespaces: %t", c.secure, c.namespaceMirroring)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			if !cfg.EnableEnterprise && c.namespaceMirroring {
				t.Skipf("skipping this test because -enable-enterprise is not set")
			}

			newOpenshiftClusterWithHelmValues(t, cfg, c.secure, c.namespaceMirroring, consulAPIGatewayHelmValues)

			serviceNamespace, serviceK8SOptions := createNamespace(t, ctx, cfg)
			certificateNamespace, certificateK8SOptions := createNamespace(t, ctx, cfg)
			gatewayNamespace, gatewayK8SOptions := createNamespace(t, ctx, cfg)
			routeNamespace, routeK8SOptions := createNamespace(t, ctx, cfg)

			logger.Logf(t, "creating target server in %s namespace", serviceNamespace)
			frameworkk8s.DeployKustomize(t, serviceK8SOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Logf(t, "creating certificate resources in %s namespace", certificateNamespace)
			applyFixture(t, cfg, certificateK8SOptions, "cases/api-gateways/certificate")

			logger.Logf(t, "creating consul.hashicorp.com gateway in %s namespace", gatewayNamespace)
			applyFixture(t, cfg, gatewayK8SOptions, "cases/consul-api-gateways/gateway")

			logger.Logf(t, "creating consul.hashicorp.com route resources in %s namespace", routeNamespace)
			applyFixture(t, cfg, routeK8SOptions, "cases/consul-api-gateways/httproute")

			logger.Log(t, "patching certificate with generated data")
			certificate := generateCertificate(t, nil, "gateway.test.local")
			frameworkk8s.RunKubectl(t, certificateK8SOptions, "patch", "secret", "certificate", "-p", fmt.Sprintf(`{"data":{"tls.crt":"%s","tls.key":"%s"}}`, base64.StdEncoding.EncodeToString(certificate.CertPEM), base64.StdEncoding.EncodeToString(certificate.PrivateKeyPEM)), "--type=merge")

			logger.Log(t, "patching gateway to certificate")
			frameworkk8s.RunKubectl(t, gatewayK8SOptions, "patch", consulGatewayResource, "gateway", "-p", fmt.Sprintf(`{"spec":{"listeners":[{"protocol":"HTTPS","port":8082,"name":"https","tls":{"certificateRefs":[{"name":"certificate","namespace":"%s"}]},"allowedRoutes":{"namespaces":{"from":"All"}}}]}}`, certificateNamespace), "--type=merge")

			logger.Log(t, "patching route to target server")
			frameworkk8s.RunKubectl(t, routeK8SOptions, "patch", consulHTTPRouteResource, "route", "-p", fmt.Sprintf(`{"spec":{"rules":[{"backendRefs":[{"name":"static-server","namespace":"%s","port":80}]}]}}`, serviceNamespace), "--type=merge")

			logger.Log(t, "patching route to gateway")
			frameworkk8s.RunKubectl(t, routeK8SOptions, "patch", consulHTTPRouteResource, "route", "-p", fmt.Sprintf(`{"spec":{"parentRefs":[{"group":"consul.hashicorp.com","kind":"Gateway","name":"gateway","namespace":"%s"}]}}`, gatewayNamespace), "--type=merge")

			k8sClient := consulGatewayRuntimeClient(t, ctx)

			consulCluster := consul.NewHelmCluster(t, map[string]string{}, ctx, cfg, "")
			consulCluster.SetNamespace("consul")
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			// Without ReferenceGrants the gateway cannot use the certificate and the route
			// cannot reach the service, so nothing should be synced to Consul.
			retryCheck(t, 120, func(r *retry.R) {
				var gateway consulgwv1beta1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: gatewayNamespace}, &gateway)
				require.NoError(r, err)

				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Conditions, falseCondition("Programmed", "Pending"))
				checkStatusCondition(r, gateway.Status.Conditions, falseCondition("Synced", "SyncError"))

				require.Len(r, gateway.Status.Listeners, 1)
				require.EqualValues(r, 1, gateway.Status.Listeners[0].AttachedRoutes)
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("ResolvedRefs", "RefNotPermitted"))
			})
			checkConsulNotExists(t, consulClient, api.APIGateway, "gateway", namespaceForConsul(c.namespaceMirroring, gatewayNamespace))

			retryCheck(t, 60, func(r *retry.R) {
				var httproute consulgwv1beta1.HTTPRoute
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "route", Namespace: routeNamespace}, &httproute)
				require.NoError(r, err)

				require.Len(r, httproute.Status.Parents, 1)
				require.EqualValues(r, consulGatewayClassControllerName, httproute.Status.Parents[0].ControllerName)
				require.EqualValues(r, "gateway", httproute.Status.Parents[0].ParentRef.Name)
				require.NotNil(r, httproute.Status.Parents[0].ParentRef.Namespace)
				require.EqualValues(r, gatewayNamespace, *httproute.Status.Parents[0].ParentRef.Namespace)
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, falseCondition("ResolvedRefs", "RefNotPermitted"))
			})
			checkConsulNotExists(t, consulClient, api.FileSystemCertificate, "certificate", namespaceForConsul(c.namespaceMirroring, certificateNamespace))

			createConsulReferenceGrant(t, cfg, k8sClient, "gateway-certificate", gatewayNamespace, certificateNamespace)
			createConsulReferenceGrant(t, cfg, k8sClient, "route-service", routeNamespace, serviceNamespace)

			retryCheck(t, 60, func(r *retry.R) {
				var gateway consulgwv1beta1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: gatewayNamespace}, &gateway)
				require.NoError(r, err)

				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Programmed", "Programmed"))
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Synced", "Synced"))
				require.Len(r, gateway.Status.Listeners, 1)
				require.EqualValues(r, 1, gateway.Status.Listeners[0].AttachedRoutes)
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
			})

			retryCheck(t, 30, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "gateway", &api.QueryOptions{
					Namespace: namespaceForConsul(c.namespaceMirroring, gatewayNamespace),
				})
				require.NoError(r, err)
				gateway := entry.(*api.APIGatewayConfigEntry)

				require.EqualValues(r, "gateway", gateway.Meta["k8s-name"])
				require.EqualValues(r, gatewayNamespace, gateway.Meta["k8s-namespace"])
				require.Len(r, gateway.Listeners, 1)
				checkConsulStatusCondition(r, gateway.Status.Conditions, trueConsulCondition("Accepted", "Accepted"))
				checkConsulStatusCondition(r, gateway.Status.Conditions, trueConsulCondition("ResolvedRefs", "ResolvedRefs"))
			})

			retryCheck(t, 30, func(r *retry.R) {
				var httproute consulgwv1beta1.HTTPRoute
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "route", Namespace: routeNamespace}, &httproute)
				require.NoError(r, err)

				require.Len(r, httproute.Status.Parents, 1)
				require.EqualValues(r, consulGatewayClassControllerName, httproute.Status.Parents[0].ControllerName)
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
			})

			retryCheck(t, 30, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, "route", &api.QueryOptions{
					Namespace: namespaceForConsul(c.namespaceMirroring, routeNamespace),
				})
				require.NoError(r, err)
				route := entry.(*api.HTTPRouteConfigEntry)

				require.EqualValues(r, "route", route.Meta["k8s-name"])
				require.EqualValues(r, routeNamespace, route.Meta["k8s-namespace"])
				require.Len(r, route.Parents, 1)
			})

			retryCheck(t, 30, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.FileSystemCertificate, "certificate", &api.QueryOptions{
					Namespace: namespaceForConsul(c.namespaceMirroring, certificateNamespace),
				})
				require.NoError(r, err)
				certificate := entry.(*api.FileSystemCertificateConfigEntry)

				require.EqualValues(r, "certificate", certificate.Meta["k8s-name"])
				require.EqualValues(r, certificateNamespace, certificate.Meta["k8s-namespace"])
			})
		})
	}
}

// TestOpenshift_ConsulAPIGateway_CoexistsWithLegacyGateway verifies that with the
// new controller enabled, the existing gateway.networking.k8s.io controller keeps
// working, and that each controller only reconciles resources for its own
// GatewayClass. This is the steady state customers run in after the upgrade.
func TestOpenshift_ConsulAPIGateway_CoexistsWithLegacyGateway(t *testing.T) {
	cfg := suite.Config()
	newOpenshiftClusterWithHelmValues(t, cfg, true, false, consulAPIGatewayHelmValues)
	logf.SetLogger(logr.New(nil))

	ctx := suite.Environment().DefaultContext(t)
	k8sClient := consulGatewayRuntimeClient(t, ctx)

	requireConsulGatewayClassAccepted(t, k8sClient, consulGatewayClassName)

	applyManifestDirs(t, cfg, "../fixtures/cases/openshift/basic/backend.yaml", "../fixtures/cases/openshift/basic/frontend.yaml",
		"../fixtures/cases/openshift/consul-api-gateway-upgrade/intentions.yaml",
		"../fixtures/cases/openshift/basic/gateway.yaml", "../fixtures/cases/openshift/basic/route.yaml",
		"../fixtures/cases/openshift/consul-api-gateway-basic/gateway.yaml", "../fixtures/cases/openshift/consul-api-gateway-basic/route.yaml")

	legacyIP := waitForLegacyGatewayAddress(t, k8sClient, "api-gateway", "consul")
	consulIP := waitForConsulGatewayAddress(t, k8sClient, "consul-api-gateway", "consul")

	requireFrontendReachable(t, legacyIP)
	requireFrontendReachable(t, consulIP)

	// Each controller must only write status for its own resources.
	retryCheck(t, 60, func(r *retry.R) {
		var legacyRoute gwv1beta1.HTTPRoute
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: "frontend-route-default", Namespace: "consul"}, &legacyRoute))
		require.Len(r, legacyRoute.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, legacyRoute.Status.Parents[0].ControllerName)

		var consulRoute consulgwv1beta1.HTTPRoute
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: "frontend-route-consul", Namespace: "consul"}, &consulRoute))
		require.Len(r, consulRoute.Status.Parents, 1)
		require.EqualValues(r, consulGatewayClassControllerName, consulRoute.Status.Parents[0].ControllerName)
	})

	// Deleting the new gateway must not affect the legacy one.
	frameworkk8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", consulGatewayResource, "consul-api-gateway", "-n", "consul")
	retryCheck(t, 60, func(r *retry.R) {
		var deployment appsv1.Deployment
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "consul-api-gateway", Namespace: "consul"}, &deployment)
		require.Error(r, err, "expected consul-api-gateway deployment to be removed")
	})
	requireFrontendReachable(t, legacyIP)
}

// TestOpenshift_ConsulAPIGateway_HelmUpgrade covers upgrading an existing OpenShift
// installation that only uses the legacy gateway.networking.k8s.io API Gateway to a
// configuration that enables the new Consul API Gateway. Each case corresponds to a
// supported combination of the upgrade flags in values.yaml:
//
//   - consulapi.enabled: enable the new controller on an existing install.
//   - upgradeTo419From418 + enableTcpRoute: prepare a 4.18 cluster for 4.19.
//   - isOcpGreaterthan4_18 + enableTcpRoute: cluster already on 4.19+. With
//     isOcpGreaterthan4_18 the chart stops rendering the gateway.networking.k8s.io
//     CRDs (the platform owns them), so this case is only run on OCP >= 4.19 and the
//     base install already sets isOcpGreaterthan4_18. Running it on an older cluster
//     would make Helm delete those CRDs together with every legacy Gateway.
//
// For all cases, global.generateManifests=true runs the pre-upgrade
// generate-manifests Job that copies existing Consul-owned Gateway API objects into
// the consul.hashicorp.com API group (suffixed with "-consul"), and the post-upgrade
// Job applies them. Afterwards the legacy gateway must still serve traffic, the new
// copy must be created, accepted and serve traffic too, and a further no-op upgrade
// must be idempotent.
func TestOpenshift_ConsulAPIGateway_HelmUpgrade(t *testing.T) {
	cases := []struct {
		name          string
		installValues map[string]string
		upgradeValues map[string]string
		// expectTCPRouteWatch is whether the legacy controller watches TCPRoutes after the upgrade.
		expectTCPRouteWatch bool
		requiresOCP419      bool
	}{
		{
			name: "consulapi enabled",
			upgradeValues: map[string]string{
				"global.openshift.crds.consulapi.enabled": "true",
			},
		},
		{
			name: "upgrade from OCP 4.18 to 4.19 with TCPRoute",
			upgradeValues: map[string]string{
				"global.openshift.upgradeTo419From418": "true",
				"global.openshift.crds.enableTcpRoute": "true",
			},
			expectTCPRouteWatch: true,
		},
		{
			name: "OCP greater than 4.18 with TCPRoute",
			installValues: map[string]string{
				"global.openshift.isOcpGreaterthan4_18": "true",
			},
			upgradeValues: map[string]string{
				"global.openshift.isOcpGreaterthan4_18": "true",
				"global.openshift.crds.enableTcpRoute":  "true",
			},
			requiresOCP419: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)
			logf.SetLogger(logr.New(nil))

			if c.requiresOCP419 && !openshiftVersionAtLeast(t, 4, 19) {
				t.Skip("skipping: requires OpenShift >= 4.19, which provides the gateway.networking.k8s.io CRDs")
			}

			// Step 1: install with the legacy API Gateway only.
			newOpenshiftClusterWithHelmValues(t, cfg, true, false, c.installValues)
			k8sClient := consulGatewayRuntimeClient(t, ctx)

			requireCRDAbsent(t, ctx, consulGatewayResource)

			applyManifestDirs(t, cfg, "../fixtures/cases/openshift/basic/backend.yaml", "../fixtures/cases/openshift/basic/frontend.yaml",
				"../fixtures/cases/openshift/consul-api-gateway-upgrade/intentions.yaml",
				"../fixtures/cases/openshift/basic/gateway.yaml", "../fixtures/cases/openshift/basic/route.yaml")

			legacyIP := waitForLegacyGatewayAddress(t, k8sClient, "api-gateway", "consul")
			requireFrontendReachable(t, legacyIP)

			// Step 2: upgrade, migrating Consul-owned objects to consul.hashicorp.com.
			upgradeValues := map[string]string{"global.generateManifests": "true"}
			for k, v := range c.upgradeValues {
				upgradeValues[k] = v
			}
			logger.Logf(t, "upgrading Consul with %v", upgradeValues)
			upgradeOpenshiftCluster(t, cfg, true, false, upgradeValues)

			// The migrated objects are not owned by any fixture so make sure we remove them.
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				for _, obj := range []string{
					consulHTTPRouteResource + "/frontend-route-default-consul",
					consulReferenceGrantResource + "/service-grant-consul",
					consulGatewayResource + "/api-gateway-consul",
				} {
					ns := "consul"
					if strings.HasPrefix(obj, consulReferenceGrantResource) {
						ns = "frontend"
					}
					_, _ = exec.Command("kubectl", "delete", obj, "-n", ns, "--ignore-not-found").CombinedOutput()
				}
			})

			requireConnectInjectArg(t, ctx, "-enable-custom-gateway-crd-controller=true", true)
			requireConnectInjectArg(t, ctx, "-enabe-tcp-route=true", c.expectTCPRouteWatch)
			requireConsulGatewayClassAccepted(t, k8sClient, consulGatewayClassName)

			// Step 3: legacy gateway must be untouched by the upgrade.
			requireFrontendReachable(t, legacyIP)

			// Step 4: migrated gateway and route must be reconciled by the new controller.
			retryCheck(t, 60, func(r *retry.R) {
				var gateway consulgwv1beta1.Gateway
				require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: "api-gateway-consul", Namespace: "consul"}, &gateway))
				require.EqualValues(r, consulGatewayClassName, gateway.Spec.GatewayClassName)
			})
			consulIP := waitForConsulGatewayAddress(t, k8sClient, "api-gateway-consul", "consul")
			requireConsulRouteAccepted(t, k8sClient, "frontend-route-default-consul", "consul", "api-gateway-consul", "consul")

			var grant consulgwv1beta1.ReferenceGrant
			require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Name: "service-grant-consul", Namespace: "frontend"}, &grant))
			require.Len(t, grant.Spec.From, 1)
			require.EqualValues(t, consulAPIGroup, grant.Spec.From[0].Group)

			requireFrontendReachable(t, consulIP)

			// Step 5: a repeated upgrade with the same values must be a no-op.
			upgradeOpenshiftCluster(t, cfg, true, false, upgradeValues)
			requireFrontendReachable(t, legacyIP)
			requireFrontendReachable(t, consulIP)
		})
	}
}

// consulGatewayRuntimeClient returns a controller-runtime client whose scheme
// also understands the consul.hashicorp.com Gateway API types. The shared
// framework client is not reused because its scheme doesn't include them.
func consulGatewayRuntimeClient(t *testing.T, ctx environment.TestContext) client.Client {
	t.Helper()

	options := ctx.KubectlOptions(t)
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)
	restConfig, err := k8s.LoadApiClientConfigE(configPath, options.ContextName)
	require.NoError(t, err)

	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, consulgwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	c, err := client.New(restConfig, client.Options{Scheme: s})
	require.NoError(t, err)
	return c
}

// applyManifestDirs applies the given files/directories (in order) with retries,
// since webhooks and CRDs may not be ready immediately after install/upgrade,
// and registers their deletion (in reverse order) on cleanup.
func applyManifestDirs(t *testing.T, cfg *config.TestConfig, paths ...string) {
	t.Helper()

	for _, p := range paths {
		p := p
		retry.RunWith(&retry.Counter{Count: 10, Wait: 30 * time.Second}, t, func(r *retry.R) {
			output, err := exec.Command("kubectl", "apply", "-f", p).CombinedOutput()
			require.NoErrorf(r, err, "failed to apply %s: %s", p, string(output))
		})
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			output, err := exec.Command("kubectl", "delete", "--ignore-not-found", "-f", p).CombinedOutput()
			assert.NoErrorf(t, err, "failed to delete %s: %s", p, string(output))
		})
	}
}

func requireConsulGatewayClassAccepted(t *testing.T, k8sClient client.Client, name string) {
	t.Helper()

	retry.RunWith(&retry.Counter{Count: 60, Wait: 5 * time.Second}, t, func(r *retry.R) {
		var gc consulgwv1beta1.CustomGatewayClass
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, &gc))
		require.EqualValues(r, consulGatewayClassControllerName, gc.Spec.ControllerName)
		checkStatusCondition(r, gc.Status.Conditions, trueCondition("Accepted", "Accepted"))
	})
}

func waitForConsulGatewayAddress(t *testing.T, k8sClient client.Client, name, namespace string) string {
	t.Helper()

	var address string
	// On startup the controller can take upwards of 1m to perform leader election.
	retry.RunWith(&retry.Counter{Count: 10, Wait: 120 * time.Second}, t, func(r *retry.R) {
		var gateway consulgwv1beta1.Gateway
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &gateway))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Programmed", "Programmed"))
		require.Len(r, gateway.Status.Addresses, 1)
		address = gateway.Status.Addresses[0].Value
	})
	return address
}

func waitForLegacyGatewayAddress(t *testing.T, k8sClient client.Client, name, namespace string) string {
	t.Helper()

	var address string
	retry.RunWith(&retry.Counter{Count: 10, Wait: 120 * time.Second}, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &gateway))
		require.Len(r, gateway.Status.Addresses, 1)
		address = gateway.Status.Addresses[0].Value
	})
	return address
}

func requireConsulRouteAccepted(t *testing.T, k8sClient client.Client, routeName, routeNamespace, gatewayName, gatewayNamespace string) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		var route consulgwv1beta1.HTTPRoute
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: routeName, Namespace: routeNamespace}, &route))
		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, consulGatewayClassControllerName, route.Status.Parents[0].ControllerName)
		require.EqualValues(r, gatewayName, route.Status.Parents[0].ParentRef.Name)
		ns := routeNamespace
		if route.Status.Parents[0].ParentRef.Namespace != nil {
			ns = string(*route.Status.Parents[0].ParentRef.Namespace)
		}
		require.Equal(r, gatewayNamespace, ns)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
	})
}

// requireFrontendReachable issues HTTPS requests to the gateway and checks the
// Gateway --> frontend --> backend path, as in TestOpenshift_Basic.
func requireFrontendReachable(t *testing.T, gatewayIP string) {
	t.Helper()

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	type upstreamCall struct {
		Body string `json:"body"`
		Code int    `json:"code"`
		Name string `json:"name"`
	}
	var body struct {
		Body          string                  `json:"body"`
		Code          int                     `json:"code"`
		Name          string                  `json:"name"`
		UpstreamCalls map[string]upstreamCall `json:"upstream_calls"`
		URI           string                  `json:"uri"`
	}

	retry.RunWith(&retry.Counter{Count: 10, Wait: 120 * time.Second}, t, func(r *retry.R) {
		resp, err := httpClient.Get("https://" + gatewayIP)
		require.NoErrorf(r, err, "request to API gateway failed: %s", err)
		defer resp.Body.Close()
		require.Equalf(r, http.StatusOK, resp.StatusCode, "request to API gateway returned failure code: %d", resp.StatusCode)
		require.NoError(r, json.NewDecoder(resp.Body).Decode(&body))
	})

	assert.Equal(t, "Hello World", body.Body)
	assert.Equal(t, 200, body.Code)
	assert.Equal(t, "frontend", body.Name)
	assert.Equal(t, "/", body.URI)

	require.Len(t, body.UpstreamCalls, 1)
	require.Contains(t, body.UpstreamCalls, "http://backend.backend:8080")
	backend := body.UpstreamCalls["http://backend.backend:8080"]
	assert.Equal(t, 200, backend.Code)
	assert.Equal(t, "backend", backend.Name)
}

// requireConnectInjectArg checks whether the connect-injector deployment's
// command contains the given flag.
func requireConnectInjectArg(t *testing.T, ctx environment.TestContext, arg string, expected bool) {
	t.Helper()

	retryCheck(t, 30, func(r *retry.R) {
		deployment, err := ctx.KubernetesClient(t).AppsV1().Deployments("consul").Get(context.Background(), "consul-connect-injector", metav1.GetOptions{})
		require.NoError(r, err)
		var cmd string
		for _, c := range deployment.Spec.Template.Spec.Containers {
			cmd += strings.Join(append(c.Command, c.Args...), " ")
		}
		require.Equalf(r, expected, strings.Contains(cmd, arg), "expected connect-injector command to contain %q: %t", arg, expected)
	})
}

// requireCRDAbsent waits for the CRD to be absent; CRDs from a previous test's
// helm uninstall may still be terminating.
func requireCRDAbsent(t *testing.T, ctx environment.TestContext, name string) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		out, err := frameworkk8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(t), "get", "crd", name, "--ignore-not-found", "-o", "name")
		require.NoError(r, err, out)
		require.Emptyf(r, strings.TrimSpace(out), "expected CRD %s not to be installed before the upgrade", name)
	})
}

// openshiftVersionAtLeast reports whether the cluster's OpenShift version is >= major.minor.
func openshiftVersionAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()

	output, err := exec.Command("kubectl", "get", "clusterversion", "version", "-o", "jsonpath={.status.desired.version}").CombinedOutput()
	require.NoErrorf(t, err, "failed to get OpenShift cluster version: %s", string(output))

	var gotMajor, gotMinor int
	_, err = fmt.Sscanf(strings.TrimSpace(string(output)), "%d.%d", &gotMajor, &gotMinor)
	require.NoErrorf(t, err, "failed to parse OpenShift cluster version %q", string(output))
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

// createConsulReferenceGrant is createReferenceGrant for the consul.hashicorp.com
// API group used by the new Consul API Gateway.
func createConsulReferenceGrant(t *testing.T, cfg *config.TestConfig, k8sClient client.Client, name, from, to string) {
	t.Helper()

	require.NoError(t, k8sClient.Create(context.Background(), &consulgwv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: to},
		Spec: consulgwv1beta1.ReferenceGrantSpec{
			From: []consulgwv1beta1.ReferenceGrantFrom{
				{Group: consulAPIGroup, Kind: "Gateway", Namespace: consulgwv1beta1.Namespace(from)},
				{Group: consulAPIGroup, Kind: "HTTPRoute", Namespace: consulgwv1beta1.Namespace(from)},
			},
			To: []consulgwv1beta1.ReferenceGrantTo{
				{Kind: "Service"},
				{Kind: "Secret"},
			},
		},
	}))

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		output, err := exec.Command("kubectl", "delete", consulReferenceGrantResource, name, "-n", to).CombinedOutput()
		assert.NoErrorf(t, err, "failed to delete resources: %s", string(output))
	})
}
