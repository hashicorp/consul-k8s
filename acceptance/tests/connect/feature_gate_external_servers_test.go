// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// TestFeatureGate_ExternalServers runs the rendered hook as a Kubernetes Job.
// Unlike a hook with no configured gates, these Jobs must perform an authorized write.
func TestFeatureGate_ExternalServers(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	options := ctx.KubectlOptions(t)
	namespace := options.Namespace
	clientset := ctx.KubernetesClient(t)
	requestCtx := context.Background()
	const gateName = "api-gateway-upstream-routing"

	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%t", secure), func(t *testing.T) {
			release := helpers.RandomName()
			serverValues := map[string]string{
				"global.acls.manageSystemACLs": "true",
				"global.tls.enabled":           fmt.Sprint(secure),
				"connectInject.enabled":        "false",
				"connectInject.cni.enabled":    "false",
				"server.replicas":              "3",
				"server.affinity":              "null",
			}
			cluster := consul.NewHelmCluster(t, serverValues, ctx, cfg, release)
			cluster.Create(t)
			bootstrapSecret := release + "-consul-bootstrap-acl-token"
			secret, err := clientset.CoreV1().Secrets(namespace).Get(requestCtx, bootstrapSecret, metav1.GetOptions{})
			require.NoError(t, err)
			require.NotEmpty(t, secret.Data["token"])
			consulClient, address := cluster.SetupConsulClient(t, secure)
			if !secure {
				consulClient, err = api.NewClient(&api.Config{Address: address, Token: string(secret.Data["token"])})
				require.NoError(t, err)
			}
			_, _, err = consulClient.Operator().FeatureGateGet(gateName, nil)
			require.NoError(t, err, "the test image must support the feature-gate API")

			pods, err := clientset.CoreV1().Pods(namespace).List(requestCtx, metav1.ListOptions{
				LabelSelector: "component=server,release=" + release,
			})
			require.NoError(t, err)
			require.Len(t, pods.Items, 3)
			var ips []string
			for _, pod := range pods.Items {
				require.NotEmpty(t, pod.Status.PodIP)
				ips = append(ips, pod.Status.PodIP)
			}

			hookRelease := helpers.RandomName()
			discoveryValues := map[string]string{
				"server.enabled":           "false",
				"externalServers.enabled":  "true",
				"externalServers.hosts[0]": fmt.Sprintf("provider=k8s namespace=%s", namespace),
			}
			renderOptions := &helm.Options{SetValues: discoveryValues, KubectlOptions: options}
			var account corev1.ServiceAccount
			require.NoError(t, yaml.Unmarshal([]byte(helm.RenderTemplate(t, renderOptions,
				config.HelmChartPath, hookRelease, []string{"templates/feature-gate-set-serviceaccount.yaml"})), &account))
			account.Annotations = nil
			serviceAccount := account.Name
			_, err = clientset.CoreV1().ServiceAccounts(namespace).Create(requestCtx, &account, metav1.CreateOptions{})
			require.NoError(t, err)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				require.NoError(t, clientset.CoreV1().ServiceAccounts(namespace).Delete(requestCtx, serviceAccount, metav1.DeleteOptions{}))
			})

			var discoveryRole rbacv1.Role
			require.NoError(t, yaml.Unmarshal([]byte(helm.RenderTemplate(t, renderOptions,
				config.HelmChartPath, hookRelease, []string{"templates/feature-gate-set-role.yaml"})), &discoveryRole))
			discoveryRole.Annotations = nil
			_, err = clientset.RbacV1().Roles(namespace).Create(requestCtx, &discoveryRole, metav1.CreateOptions{})
			require.NoError(t, err)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				require.NoError(t, clientset.RbacV1().Roles(namespace).Delete(requestCtx, serviceAccount, metav1.DeleteOptions{}))
			})
			var discoveryBinding rbacv1.RoleBinding
			require.NoError(t, yaml.Unmarshal([]byte(helm.RenderTemplate(t, renderOptions,
				config.HelmChartPath, hookRelease, []string{"templates/feature-gate-set-rolebinding.yaml"})), &discoveryBinding))
			discoveryBinding.Annotations = nil
			_, err = clientset.RbacV1().RoleBindings(namespace).Create(requestCtx, &discoveryBinding, metav1.CreateOptions{})
			require.NoError(t, err)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				require.NoError(t, clientset.RbacV1().RoleBindings(namespace).Delete(requestCtx, serviceAccount, metav1.DeleteOptions{}))
			})

			policy, _, err := consulClient.ACL().PolicyCreate(&api.ACLPolicy{
				Name: serviceAccount, Rules: `operator = "write"`,
			}, nil)
			require.NoError(t, err)
			role, _, err := consulClient.ACL().RoleCreate(&api.ACLRole{
				Name: serviceAccount, Policies: []*api.ACLRolePolicyLink{{ID: policy.ID}},
			}, nil)
			require.NoError(t, err)
			methods, _, err := consulClient.ACL().AuthMethodList(nil)
			require.NoError(t, err)
			var authMethod string
			for _, method := range methods {
				if method.Type == "kubernetes" && strings.HasSuffix(method.Name, "-k8s-component-auth-method") {
					authMethod = method.Name
					break
				}
			}
			require.NotEmpty(t, authMethod, "server-acl-init must create a component Kubernetes auth method")
			_, _, err = consulClient.ACL().BindingRuleCreate(&api.ACLBindingRule{
				AuthMethod: authMethod,
				Selector:   fmt.Sprintf("serviceaccount.name == %q and serviceaccount.namespace == %q", serviceAccount, namespace),
				BindType:   "role", BindName: role.Name,
			}, nil)
			require.NoError(t, err)

			hosts := []struct{ name, address string }{
				{"dns", release + "-consul-server"},
				{"ip", ips[0]},
				{"exec", "exec=echo " + strings.Join(ips, " ")},
				{"provider-k8s", fmt.Sprintf("provider=k8s namespace=%s label_selector=release=%s,component=server", namespace, release)},
			}
			for _, address := range pods.Items[0].Status.PodIPs {
				if ip := net.ParseIP(address.IP); ip != nil && ip.To4() == nil && address.IP != ips[0] {
					hosts = append(hosts, struct{ name, address string }{"ipv6", address.IP})
				}
			}
			for _, host := range hosts {
				for _, auth := range []string{"token-secret", "token-file", "kubernetes-login", "anonymous-denied", "invalid-token"} {
					t.Run("hosts="+host.name+"/auth="+auth, func(t *testing.T) {
						values, err := cfg.HelmValuesFromConfig()
						require.NoError(t, err)
						values["global.enabled"] = "false"
						values["server.enabled"] = "false"
						values["externalServers.enabled"] = "true"
						values["externalServers.hosts[0]"] = host.address
						values["externalServers.skipServerWatch"] = "true"
						values["global.tls.enabled"] = fmt.Sprint(secure)
						values["global.acls.bootstrapToken.secretName"] = bootstrapSecret
						values["global.acls.bootstrapToken.secretKey"] = "token"
						if secure {
							values["global.tls.caCert.secretName"] = release + "-consul-ca-cert"
							values["externalServers.tlsServerName"] = "server.dc1.consul"
							values["externalServers.httpsPort"] = "8501"
						} else {
							values["externalServers.httpsPort"] = "8500"
						}
						rendered := helm.RenderTemplate(t, &helm.Options{
							SetValues: values, KubectlOptions: options,
						}, config.HelmChartPath, hookRelease, []string{"templates/feature-gate-set-job.yaml"})
						var job batchv1.Job
						require.NoError(t, yaml.Unmarshal([]byte(rendered), &job))
						job.Name = helpers.RandomName()
						job.Annotations = nil
						require.Equal(t, serviceAccount, job.Spec.Template.Spec.ServiceAccountName)
						backoffLimit := int32(0)
						deadline := int64(120)
						job.Spec.BackoffLimit = &backoffLimit
						job.Spec.ActiveDeadlineSeconds = &deadline
						container := &job.Spec.Template.Spec.Containers[0]
						container.Command = []string{"consul-k8s-control-plane", "feature-gate-set"}
						container.Args = []string{"-feature-gate=" + gateName + "=true", "-timeout=60s"}
						var env []corev1.EnvVar
						for _, variable := range container.Env {
							if variable.Name != "CONSUL_ACL_TOKEN" && variable.Name != "CONSUL_ACL_TOKEN_FILE" {
								env = append(env, variable)
							}
						}
						container.Env = env
						switch auth {
						case "token-secret":
							container.Env = append(container.Env, corev1.EnvVar{
								Name: "CONSUL_ACL_TOKEN",
								ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: bootstrapSecret}, Key: "token",
								}},
							})
						case "token-file":
							job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, corev1.Volume{
								Name: "acl-token", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: bootstrapSecret}},
							})
							container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "acl-token", MountPath: "/consul/test-token", ReadOnly: true})
							container.Env = append(container.Env, corev1.EnvVar{Name: "CONSUL_ACL_TOKEN_FILE", Value: "/consul/test-token/token"})
						case "kubernetes-login":
							container.Env = append(container.Env,
								corev1.EnvVar{Name: "CONSUL_LOGIN_AUTH_METHOD", Value: authMethod},
								corev1.EnvVar{Name: "CONSUL_LOGIN_META", Value: "component=feature-gate-acceptance"},
							)
						case "invalid-token":
							container.Env = append(container.Env, corev1.EnvVar{Name: "CONSUL_ACL_TOKEN", Value: "invalid-token"})
						}

						// Reset intent so success cannot be mistaken for a previous Job's write.
						_, err = consulClient.Operator().FeatureGateSet(gateName, false, 0, nil)
						require.NoError(t, err)
						existingTokens := make(map[string]bool)
						if auth == "kubernetes-login" {
							tokens, _, err := consulClient.ACL().TokenListFiltered(api.ACLTokenFilterOptions{AuthMethod: authMethod}, nil)
							require.NoError(t, err)
							for _, token := range tokens {
								existingTokens[token.AccessorID] = true
							}
						}
						_, err = clientset.BatchV1().Jobs(namespace).Create(requestCtx, &job, metav1.CreateOptions{})
						require.NoError(t, err)
						helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
							k8s.WritePodsDebugInfoIfFailed(t, options, cfg.DebugDirectory, "job-name="+job.Name)
							propagation := metav1.DeletePropagationForeground
							require.NoError(t, clientset.BatchV1().Jobs(namespace).Delete(requestCtx, job.Name, metav1.DeleteOptions{PropagationPolicy: &propagation}))
						})
						success := auth != "anonymous-denied" && auth != "invalid-token"
						condition := "complete"
						if !success {
							condition = "failed"
						}
						k8s.RunKubectl(t, options, "wait", "--for=condition="+condition, "--timeout=150s", "job/"+job.Name)
						actual, _, err := consulClient.Operator().FeatureGateGet(gateName, nil)
						require.NoError(t, err)
						require.Equal(t, success, actual.DesiredEnabled)
						if !success {
							logs, err := k8s.RunKubectlAndGetOutputE(t, options, "logs", "job/"+job.Name)
							require.NoError(t, err)
							require.Contains(t, logs, "Error setting feature gate")
						}
						if auth == "kubernetes-login" {
							require.Eventually(t, func() bool {
								tokens, _, err := consulClient.ACL().TokenListFiltered(api.ACLTokenFilterOptions{AuthMethod: authMethod}, nil)
								if err != nil {
									return false
								}
								for _, token := range tokens {
									if !existingTokens[token.AccessorID] {
										return false
									}
								}
								return true
							}, 10*time.Second, time.Second, "the Job must log out its temporary token")
						}
					})
				}
			}
		})
	}
}
