// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package convertmultiportservices

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestTranslateDeployment(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		deployment  *appsv1.Deployment
		wantPort    string
		wantChanged bool
		wantError   string
	}{
		"missing annotation selects first container port": {
			deployment:  conversionDeployment("test", "default", nil, applicationContainers(2)),
			wantPort:    "port-0",
			wantChanged: true,
		},
		"named default is selected": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationPort:        "port-0,port-1",
				constants.AnnotationDefaultPort: "port-1",
			}, applicationContainers(2)),
			wantPort:    "port-1",
			wantChanged: true,
		},
		"numeric default maps to named port": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationPort:        "port-0,port-1",
				constants.AnnotationDefaultPort: "8081",
			}, applicationContainers(2)),
			wantPort:    "port-1",
			wantChanged: true,
		},
		"unnamed port is written numerically": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationPort: "8080,port-1",
			}, []corev1.Container{{Name: "app", Ports: []corev1.ContainerPort{
				{ContainerPort: 8080}, {Name: "port-1", ContainerPort: 8081},
			}}}),
			wantPort:    "8080",
			wantChanged: true,
		},
		"already selected one port is unchanged": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationPort: "port-1",
			}, applicationContainers(2)),
			wantChanged: false,
		},
		"port named on a later container is translated": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationPort: "port-0,port-1",
			}, applicationContainers(1, 2)),
			wantPort:    "port-0",
			wantChanged: true,
		},
		"first container without ports falls back to later containers": {
			deployment:  conversionDeployment("test", "default", nil, applicationContainers(0, 2)),
			wantPort:    "port-0",
			wantChanged: true,
		},
		"single-port deployment is unchanged": {
			deployment:  conversionDeployment("test", "default", nil, applicationContainers(1)),
			wantChanged: false,
		},
		"multi-port first container is translated when a later container is single-port": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationPort: "port-0,port-1",
			}, applicationContainers(2, 1)),
			wantPort:    "port-0",
			wantChanged: true,
		},
		"legacy multiple-service deployment is unchanged": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationService: "api,metrics",
				constants.AnnotationPort:    "port-0,port-1",
			}, applicationContainers(2)),
			wantChanged: false,
		},
		"invalid configured default fails instead of silently choosing a port": {
			deployment: conversionDeployment("test", "default", map[string]string{
				constants.AnnotationPort:        "port-0,port-1",
				constants.AnnotationDefaultPort: "missing",
			}, applicationContainers(2)),
			wantError: "is not one of the service ports this workload would register",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			selected, changed, err := translateTemplate(&tt.deployment.Spec.Template)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantChanged, changed)
			require.Equal(t, tt.wantPort, selected)
			if changed {
				require.Equal(t, tt.wantPort, tt.deployment.Spec.Template.Annotations[constants.AnnotationPort])
			}
		})
	}
}

func TestCommandTranslateFiltersDeploymentsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		namespace("allowed", map[string]string{"conversion": "enabled"}),
		namespace("selector-excluded", nil),
		namespace("denied", map[string]string{"conversion": "enabled"}),
		namespace(metav1.NamespaceSystem, map[string]string{"conversion": "enabled"}),
		namespace(metav1.NamespacePublic, map[string]string{"conversion": "enabled"}),
		conversionDeployment("explicit", "allowed", map[string]string{
			constants.AnnotationInject:      "true",
			constants.AnnotationPort:        "port-0,port-1",
			constants.AnnotationDefaultPort: "port-1",
			"example.com/credential":        "must-not-appear-in-logs",
		}, applicationContainers(2)),
		conversionDeployment("inherited", "allowed", nil, applicationContainers(2)),
		conversionDeployment("opted-out", "allowed", map[string]string{
			constants.AnnotationInject: "false",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
		conversionDeployment("single", "allowed", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0",
		}, applicationContainers(1)),
		conversionDeployment("selector-excluded", "selector-excluded", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
		conversionDeployment("namespace-denied", "denied", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
		conversionDeployment("system", metav1.NamespaceSystem, map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
		conversionDeployment("public", metav1.NamespacePublic, map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
	)

	var logs bytes.Buffer
	cmd := conversionCommand(client, &logs)
	code := cmd.Run([]string{
		"-strategy=TRANSLATE",
		"-default-inject=true",
		"-deny-k8s-namespace=denied",
		`-namespace-selector={"matchLabels":{"conversion":"enabled"}}`,
	})
	require.Equal(t, 0, code)

	require.Equal(t, "port-1", deploymentPort(t, ctx, client, "allowed", "explicit"))
	require.Equal(t, "port-0", deploymentPort(t, ctx, client, "allowed", "inherited"))
	require.Equal(t, "port-0,port-1", deploymentPort(t, ctx, client, "allowed", "opted-out"))
	require.Equal(t, "port-0", deploymentPort(t, ctx, client, "allowed", "single"))
	require.Equal(t, "port-0,port-1", deploymentPort(t, ctx, client, "selector-excluded", "selector-excluded"))
	require.Equal(t, "port-0,port-1", deploymentPort(t, ctx, client, "denied", "namespace-denied"))
	require.Equal(t, "port-0,port-1", deploymentPort(t, ctx, client, metav1.NamespaceSystem, "system"))
	require.Equal(t, "port-0,port-1", deploymentPort(t, ctx, client, metav1.NamespacePublic, "public"))

	output := logs.String()
	require.Contains(t, output, "strategy=TRANSLATE")
	require.Contains(t, output, "namespace=allowed")
	require.Contains(t, output, "name=explicit")
	require.Contains(t, output, "kind=Deployment")
	require.Contains(t, output, "selected_port=port-1")
	require.NotContains(t, output, "must-not-appear-in-logs")

	updatesBefore := workloadUpdateCount(client.Actions())
	second := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, second.Run([]string{
		"-strategy=TRANSLATE",
		"-default-inject=true",
		"-deny-k8s-namespace=denied",
		`-namespace-selector={"matchLabels":{"conversion":"enabled"}}`,
	}))
	require.Equal(t, updatesBefore, workloadUpdateCount(client.Actions()))
}

func TestCommandDecommissionAllMeshEligibleDeployments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionDeployment("explicit-multi", "default", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
		conversionDeployment("inherited-single", "default", nil, applicationContainers(1)),
		conversionDeployment("already-out", "default", map[string]string{
			constants.AnnotationInject: "false",
		}, applicationContainers(2)),
	)

	cmd := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, cmd.Run([]string{"-strategy=DECOMMISSION", "-default-inject=true"}))
	require.Equal(t, "false", deploymentInject(t, ctx, client, "default", "explicit-multi"))
	require.Equal(t, "false", deploymentInject(t, ctx, client, "default", "inherited-single"))
	require.Equal(t, "false", deploymentInject(t, ctx, client, "default", "already-out"))

	updatesBefore := workloadUpdateCount(client.Actions())
	second := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, second.Run([]string{"-strategy=DECOMMISSION", "-default-inject=true"}))
	require.Equal(t, updatesBefore, workloadUpdateCount(client.Actions()))
}

// TestCommandConvertsEveryWorkloadKind covers the workload kinds that own a Pod
// template. A kind that the Job skips is not rejected at upgrade time; it keeps
// running until its Pods are recreated and only then fails admission, so the
// failure surfaces long after the upgrade window.
func TestCommandConvertsEveryWorkloadKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	multiPort := map[string]string{
		constants.AnnotationInject: "true",
		constants.AnnotationPort:   "port-0,port-1",
	}
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionDeployment("deploy", "default", multiPort, applicationContainers(2)),
		conversionStatefulSet("sts", "default", multiPort, applicationContainers(2)),
		conversionDaemonSet("ds", "default", multiPort, applicationContainers(2)),
		conversionStatefulSet("sts-opted-out", "default", map[string]string{
			constants.AnnotationInject: "false",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
		conversionDaemonSet("ds-single", "default", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0",
		}, applicationContainers(1)),
	)

	var logs bytes.Buffer
	cmd := conversionCommand(client, &logs)
	require.Equal(t, 0, cmd.Run([]string{"-strategy=TRANSLATE"}))

	require.Equal(t, "port-0", deploymentPort(t, ctx, client, "default", "deploy"))
	require.Equal(t, "port-0", statefulSetAnnotations(t, ctx, client, "default", "sts")[constants.AnnotationPort])
	require.Equal(t, "port-0", daemonSetAnnotations(t, ctx, client, "default", "ds")[constants.AnnotationPort])

	// Opt-out and single-port workloads are left alone for every kind.
	require.Equal(t, "port-0,port-1", statefulSetAnnotations(t, ctx, client, "default", "sts-opted-out")[constants.AnnotationPort])
	require.Equal(t, "port-0", daemonSetAnnotations(t, ctx, client, "default", "ds-single")[constants.AnnotationPort])

	output := logs.String()
	require.Contains(t, output, "kind=StatefulSet")
	require.Contains(t, output, "kind=DaemonSet")

	updatesBefore := workloadUpdateCount(client.Actions())
	second := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, second.Run([]string{"-strategy=TRANSLATE"}))
	require.Equal(t, updatesBefore, workloadUpdateCount(client.Actions()))
}

func TestCommandDecommissionEveryWorkloadKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionStatefulSet("sts", "default", nil, applicationContainers(2)),
		conversionDaemonSet("ds", "default", nil, applicationContainers(2)),
	)

	cmd := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, cmd.Run([]string{"-strategy=DECOMMISSION", "-default-inject=true"}))
	require.Equal(t, "false", statefulSetAnnotations(t, ctx, client, "default", "sts")[constants.AnnotationInject])
	require.Equal(t, "false", daemonSetAnnotations(t, ctx, client, "default", "ds")[constants.AnnotationInject])
}

// TestCommandStatefulSetListFailureIsReported guards against a partial
// conversion silently reporting success when one workload kind is unreadable.
func TestCommandStatefulSetListFailureIsReported(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset(namespace("default", nil))
	client.PrependReactor("list", "statefulsets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("statefulsets forbidden")
	})

	ui := cli.NewMockUi()
	cmd := &Command{UI: ui, k8sClient: client, logger: hclog.NewNullLogger()}
	require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE"}))
	require.Contains(t, ui.ErrorWriter.String(), "listing StatefulSets")
}

// TestCommandReportsWorkloadsItCannotRewrite covers kinds outside
// convertedWorkloadKinds. They are not rewritten, but the upgrade must fail with
// an actionable list rather than leaving them to fail admission weeks later at
// the next Pod recreation.
func TestCommandReportsWorkloadsItCannotRewrite(t *testing.T) {
	t.Parallel()
	multiPort := map[string]string{
		constants.AnnotationInject: "true",
		constants.AnnotationPort:   "port-0,port-1",
	}
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		// An Argo Rollout owns its Pods directly.
		conversionPod("rollout-abc123", "default", multiPort, controllerOwner("Rollout", "checkout")),
		// A bare Pod has no controller at all.
		conversionPod("standalone", "default", multiPort, nil),
		// A Deployment-owned Pod is reached through its ReplicaSet and must not
		// be reported, because the mutation pass already rewrote the Deployment.
		conversionPod("deploy-rs-xyz", "default", multiPort, controllerOwner("ReplicaSet", "deploy-rs")),
		conversionReplicaSet("deploy-rs", "default", controllerOwner("Deployment", "deploy")),
		conversionDeployment("deploy", "default", multiPort, applicationContainers(2)),
		// Single-port and opted-out Pods are never reported.
		conversionPod("single", "default", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0",
		}, controllerOwner("Rollout", "single-rollout")),
		conversionPod("opted-out", "default", map[string]string{
			constants.AnnotationInject: "false",
			constants.AnnotationPort:   "port-0,port-1",
		}, controllerOwner("Rollout", "opted-out-rollout")),
	)

	ui := cli.NewMockUi()
	cmd := conversionCommand(client, &bytes.Buffer{})
	cmd.UI = ui
	require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE"}))

	output := ui.ErrorWriter.String()
	require.Contains(t, output, "Rollout default/checkout")
	require.Contains(t, output, "Pod default/standalone")
	require.NotContains(t, output, "default/deploy")
	require.NotContains(t, output, "single-rollout")
	require.NotContains(t, output, "opted-out-rollout")
}

// TestCommandReportsEachStrandedOwnerOnce keeps the failure message readable
// when a controller owns many replicas.
func TestCommandReportsEachStrandedOwnerOnce(t *testing.T) {
	t.Parallel()
	multiPort := map[string]string{
		constants.AnnotationInject: "true",
		constants.AnnotationPort:   "port-0,port-1",
	}
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionPod("checkout-1", "default", multiPort, controllerOwner("Rollout", "checkout")),
		conversionPod("checkout-2", "default", multiPort, controllerOwner("Rollout", "checkout")),
		conversionPod("checkout-3", "default", multiPort, controllerOwner("Rollout", "checkout")),
	)

	ui := cli.NewMockUi()
	cmd := conversionCommand(client, &bytes.Buffer{})
	cmd.UI = ui
	require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE"}))
	require.Contains(t, ui.ErrorWriter.String(), "1 workload(s)")
	require.Equal(t, 1, strings.Count(ui.ErrorWriter.String(), "Rollout default/checkout"))
}

// TestCommandIgnoresStrandedPodsInExcludedNamespaces keeps the scan consistent
// with the namespace filtering the mutation pass applies.
func TestCommandIgnoresStrandedPodsInExcludedNamespaces(t *testing.T) {
	t.Parallel()
	multiPort := map[string]string{
		constants.AnnotationInject: "true",
		constants.AnnotationPort:   "port-0,port-1",
	}
	client := fake.NewSimpleClientset(
		namespace("consul", nil),
		namespace("denied", nil),
		namespace("default", nil),
		conversionPod("control-plane", "consul", multiPort, controllerOwner("Rollout", "consul-rollout")),
		conversionPod("blocked", "denied", multiPort, controllerOwner("Rollout", "denied-rollout")),
		conversionPod("allowed", "default", multiPort, controllerOwner("Rollout", "allowed-rollout")),
	)

	ui := cli.NewMockUi()
	cmd := conversionCommand(client, &bytes.Buffer{})
	cmd.UI = ui
	require.Equal(t, 1, cmd.Run([]string{
		"-strategy=TRANSLATE",
		"-release-namespace=consul",
		"-deny-k8s-namespace=denied",
	}))

	output := ui.ErrorWriter.String()
	require.Contains(t, output, "allowed-rollout")
	require.NotContains(t, output, "consul-rollout")
	require.NotContains(t, output, "denied-rollout")
}

// TestCommandCleanClusterSucceeds guards against the scan failing an upgrade
// that has nothing left to convert.
func TestCommandCleanClusterSucceeds(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionPod("single", "default", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0",
		}, controllerOwner("Rollout", "single-rollout")),
	)

	cmd := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, cmd.Run([]string{"-strategy=TRANSLATE"}))
}

func TestCommandEmptyAllowListDeniesEveryNamespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionDeployment("unchanged", "default", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
	)

	cmd := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, cmd.Run([]string{"-strategy=TRANSLATE", "-allow-k8s-namespace="}))
	require.Equal(t, "port-0,port-1", deploymentPort(t, ctx, client, "default", "unchanged"))
	require.Zero(t, workloadUpdateCount(client.Actions()))
}

func TestCommandFailureCases(t *testing.T) {
	t.Parallel()

	t.Run("unknown strategy", func(t *testing.T) {
		ui := cli.NewMockUi()
		cmd := &Command{UI: ui, k8sClient: fake.NewSimpleClientset()}
		require.Equal(t, 1, cmd.Run([]string{"-strategy=NONE"}))
		require.Contains(t, ui.ErrorWriter.String(), "unsupported conversion strategy")
	})

	t.Run("invalid injection annotation fails conversion", func(t *testing.T) {
		client := fake.NewSimpleClientset(
			namespace("default", nil),
			conversionDeployment("invalid", "default", map[string]string{
				constants.AnnotationInject: "sometimes",
			}, applicationContainers(2)),
		)
		ui := cli.NewMockUi()
		cmd := &Command{UI: ui, k8sClient: client, logger: hclog.NewNullLogger()}
		require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE"}))
		require.Contains(t, ui.ErrorWriter.String(), "invalid consul.hashicorp.com/connect-inject annotation")
	})

	t.Run("invalid default port fails conversion", func(t *testing.T) {
		client := fake.NewSimpleClientset(
			namespace("default", nil),
			conversionDeployment("invalid", "default", map[string]string{
				constants.AnnotationInject:      "true",
				constants.AnnotationPort:        "port-0,port-1",
				constants.AnnotationDefaultPort: "missing",
			}, applicationContainers(2)),
		)
		ui := cli.NewMockUi()
		cmd := &Command{UI: ui, k8sClient: client, logger: hclog.NewNullLogger()}
		require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE"}))
		require.Contains(t, ui.ErrorWriter.String(), "is not one of the service ports")
	})

	t.Run("Kubernetes list failure is returned", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		client.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("API unavailable")
		})
		ui := cli.NewMockUi()
		cmd := &Command{UI: ui, k8sClient: client, logger: hclog.NewNullLogger()}
		require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE"}))
		require.Contains(t, ui.ErrorWriter.String(), "API unavailable")
	})

	t.Run("invalid namespace selector is returned", func(t *testing.T) {
		ui := cli.NewMockUi()
		cmd := &Command{UI: ui, k8sClient: fake.NewSimpleClientset(), logger: hclog.NewNullLogger()}
		require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE", "-namespace-selector={"}))
		require.Contains(t, ui.ErrorWriter.String(), "decoding namespace selector")
	})
}

func TestCommandRetriesDeploymentUpdateConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionDeployment("retry", "default", map[string]string{
			constants.AnnotationInject: "true",
			constants.AnnotationPort:   "port-0,port-1",
		}, applicationContainers(2)),
	)
	attempts := 0
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		attempts++
		if attempts == 1 {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "retry", errors.New("conflict"))
		}
		return false, nil, nil
	})

	cmd := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, cmd.Run([]string{"-strategy=TRANSLATE"}))
	require.GreaterOrEqual(t, attempts, 2)
	require.Equal(t, "port-0", deploymentPort(t, ctx, client, "default", "retry"))
}

func TestCommandSkipsReleaseNamespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		namespace("consul", nil),
		namespace("default", nil),
		conversionDeployment("consul-connect-injector", "consul", nil, applicationContainers(2)),
		conversionDeployment("workload", "default", nil, applicationContainers(2)),
	)

	cmd := conversionCommand(client, &bytes.Buffer{})
	require.Equal(t, 0, cmd.Run([]string{
		"-strategy=DECOMMISSION",
		"-default-inject=true",
		"-release-namespace=consul",
	}))
	require.Empty(t, deploymentInject(t, ctx, client, "consul", "consul-connect-injector"))
	require.Equal(t, "false", deploymentInject(t, ctx, client, "default", "workload"))
}

func TestCommandContinuesPastFailingDeploymentAndStillFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		namespace("default", nil),
		conversionDeployment("aaa-broken", "default", map[string]string{
			constants.AnnotationInject:      "true",
			constants.AnnotationDefaultPort: "missing",
		}, applicationContainers(2)),
		conversionDeployment("zzz-healthy", "default", map[string]string{
			constants.AnnotationInject: "true",
		}, applicationContainers(2)),
	)

	ui := cli.NewMockUi()
	cmd := conversionCommand(client, &bytes.Buffer{})
	cmd.UI = ui
	require.Equal(t, 1, cmd.Run([]string{"-strategy=TRANSLATE"}))
	require.Contains(t, ui.ErrorWriter.String(), "is not one of the service ports")
	// The Deployment sorted after the failure is still converted, so the operator
	// is not left guessing which workloads the Job reached.
	require.Equal(t, "port-0", deploymentPort(t, ctx, client, "default", "zzz-healthy"))
}

func conversionCommand(client *fake.Clientset, output *bytes.Buffer) *Command {
	return &Command{
		UI:        cli.NewMockUi(),
		k8sClient: client,
		logger: hclog.New(&hclog.LoggerOptions{
			Output: output,
			Level:  hclog.Debug,
		}),
	}
}

func conversionDeployment(name, namespace string, annotations map[string]string, containers []corev1.Container) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       appsv1.DeploymentSpec{Template: conversionPodTemplate(annotations, containers)},
	}
}

func conversionStatefulSet(name, namespace string, annotations map[string]string, containers []corev1.Container) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       appsv1.StatefulSetSpec{Template: conversionPodTemplate(annotations, containers)},
	}
}

func conversionDaemonSet(name, namespace string, annotations map[string]string, containers []corev1.Container) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       appsv1.DaemonSetSpec{Template: conversionPodTemplate(annotations, containers)},
	}
}

func conversionPodTemplate(annotations map[string]string, containers []corev1.Container) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
		Spec:       corev1.PodSpec{Containers: containers},
	}
}

func conversionPod(name, namespace string, annotations map[string]string, owner *metav1.OwnerReference) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: annotations},
		Spec:       corev1.PodSpec{Containers: applicationContainers(2)},
	}
	if owner != nil {
		pod.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return pod
}

func conversionReplicaSet(name, namespace string, owner *metav1.OwnerReference) *appsv1.ReplicaSet {
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	if owner != nil {
		replicaSet.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return replicaSet
}

func controllerOwner(kind, name string) *metav1.OwnerReference {
	controller := true
	return &metav1.OwnerReference{Kind: kind, Name: name, Controller: &controller}
}

func applicationContainers(portCounts ...int) []corev1.Container {
	containers := make([]corev1.Container, 0, len(portCounts))
	portValue := int32(8080)
	for containerIndex, count := range portCounts {
		container := corev1.Container{Name: "container-" + strconv.Itoa(containerIndex)}
		for portIndex := 0; portIndex < count; portIndex++ {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				Name:          "port-" + strconv.Itoa(portIndex),
				ContainerPort: portValue,
			})
			portValue++
		}
		containers = append(containers, container)
	}
	return containers
}

func namespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func deploymentPort(t *testing.T, ctx context.Context, client *fake.Clientset, namespace, name string) string {
	t.Helper()
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)
	return deployment.Spec.Template.Annotations[constants.AnnotationPort]
}

func deploymentInject(t *testing.T, ctx context.Context, client *fake.Clientset, namespace, name string) string {
	t.Helper()
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)
	return deployment.Spec.Template.Annotations[constants.AnnotationInject]
}

func statefulSetAnnotations(t *testing.T, ctx context.Context, client *fake.Clientset, namespace, name string) map[string]string {
	t.Helper()
	statefulSet, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)
	return statefulSet.Spec.Template.Annotations
}

func daemonSetAnnotations(t *testing.T, ctx context.Context, client *fake.Clientset, namespace, name string) map[string]string {
	t.Helper()
	daemonSet, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)
	return daemonSet.Spec.Template.Annotations
}

func workloadUpdateCount(actions []k8stesting.Action) int {
	updates := 0
	for _, action := range actions {
		for _, resource := range []string{"deployments", "statefulsets", "daemonsets"} {
			if action.Matches("update", resource) {
				updates++
			}
		}
	}
	return updates
}
