// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"
	"fmt"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const credentialGatewayEnvoyContainer = "terminating-gateway"

// credentialGatewayPod returns a running terminating-gateway pod with the
// credential-injection containers and the given per-container readiness.
func credentialGatewayPod(ready map[string]bool) *corev1.Pod {
	pod := createGatewayPod("terminating-gateway", "1.2.3.4", map[string]string{
		constants.AnnotationGatewayKind:              terminatingGateway,
		constants.AnnotationGatewayConsulServiceName: "terminating-gateway",
	})
	pod.Status.Phase = corev1.PodRunning
	for _, name := range []string{
		credentialGatewayEnvoyContainer,
		constants.CredentialVaultAgentContainerName,
		constants.CredentialProcessorContainerName,
	} {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: name})
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: name, Ready: ready[name]})
	}
	podReady := corev1.ConditionTrue
	for _, isReady := range ready {
		if !isReady {
			podReady = corev1.ConditionFalse
		}
	}
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: podReady}}
	return pod
}

func allCredentialGatewayContainersReady() map[string]bool {
	return map[string]bool{
		credentialGatewayEnvoyContainer:             true,
		constants.CredentialVaultAgentContainerName: true,
		constants.CredentialProcessorContainerName:  true,
	}
}

func TestCredentialGatewayHealth(t *testing.T) {
	agentDown := allCredentialGatewayContainersReady()
	agentDown[constants.CredentialVaultAgentContainerName] = false

	cases := map[string]struct {
		pod            func() *corev1.Pod
		endpointHealth string
		wantHealth     string
		wantOverridden bool
	}{
		"ready pod is unchanged": {
			pod:            func() *corev1.Pod { return credentialGatewayPod(allCredentialGatewayContainersReady()) },
			endpointHealth: api.HealthPassing,
			wantHealth:     api.HealthPassing,
		},
		"only the Vault Agent is not ready": {
			pod:            func() *corev1.Pod { return credentialGatewayPod(agentDown) },
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthPassing,
			wantOverridden: true,
		},
		"processor not ready": {
			pod: func() *corev1.Pod {
				ready := allCredentialGatewayContainersReady()
				ready[constants.CredentialProcessorContainerName] = false
				return credentialGatewayPod(ready)
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"Envoy not ready": {
			pod: func() *corev1.Pod {
				ready := allCredentialGatewayContainersReady()
				ready[credentialGatewayEnvoyContainer] = false
				return credentialGatewayPod(ready)
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"Vault Agent and processor not ready": {
			pod: func() *corev1.Pod {
				ready := allCredentialGatewayContainersReady()
				ready[constants.CredentialVaultAgentContainerName] = false
				ready[constants.CredentialProcessorContainerName] = false
				return credentialGatewayPod(ready)
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"terminating pod": {
			pod: func() *corev1.Pod {
				pod := credentialGatewayPod(agentDown)
				pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"pod not running yet": {
			pod: func() *corev1.Pod {
				pod := credentialGatewayPod(agentDown)
				pod.Status.Phase = corev1.PodPending
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"missing container status": {
			pod: func() *corev1.Pod {
				pod := credentialGatewayPod(agentDown)
				pod.Status.ContainerStatuses = pod.Status.ContainerStatuses[1:]
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"readiness gate not satisfied": {
			pod: func() *corev1.Pod {
				pod := credentialGatewayPod(agentDown)
				pod.Spec.ReadinessGates = []corev1.PodReadinessGate{{ConditionType: "example.com/ready"}}
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"readiness gate satisfied": {
			pod: func() *corev1.Pod {
				pod := credentialGatewayPod(agentDown)
				pod.Spec.ReadinessGates = []corev1.PodReadinessGate{{ConditionType: "example.com/ready"}}
				pod.Status.Conditions = append(pod.Status.Conditions,
					corev1.PodCondition{Type: "example.com/ready", Status: corev1.ConditionTrue})
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthPassing,
			wantOverridden: true,
		},
		"native sidecar not ready": {
			pod: func() *corev1.Pod {
				pod := credentialGatewayPod(agentDown)
				always := corev1.ContainerRestartPolicyAlways
				pod.Spec.InitContainers = []corev1.Container{{Name: "sidecar", RestartPolicy: &always}}
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "sidecar", Ready: false}}
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"terminating gateway without credential injection": {
			pod: func() *corev1.Pod {
				pod := createGatewayPod("terminating-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayKind: terminatingGateway,
				})
				pod.Status.Phase = corev1.PodRunning
				pod.Spec.Containers = []corev1.Container{{Name: credentialGatewayEnvoyContainer}}
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: credentialGatewayEnvoyContainer, Ready: true}}
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
		"Kubernetes Secret source has no Vault Agent": {
			pod: func() *corev1.Pod {
				pod := credentialGatewayPod(allCredentialGatewayContainersReady())
				pod.Spec.Containers = []corev1.Container{{Name: credentialGatewayEnvoyContainer}, {Name: constants.CredentialProcessorContainerName}}
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{
					{Name: credentialGatewayEnvoyContainer, Ready: true},
					{Name: constants.CredentialProcessorContainerName, Ready: true},
				}
				return pod
			},
			endpointHealth: api.HealthCritical,
			wantHealth:     api.HealthCritical,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			health, overridden := credentialGatewayHealth(*tc.pod(), tc.endpointHealth)
			require.Equal(t, tc.wantHealth, health)
			require.Equal(t, tc.wantOverridden, overridden)
		})
	}
}

// TestReconcile_CredentialGatewayIgnoresVaultAgentReadiness registers gateway
// pods against a real Consul test server and checks the resulting health check.
func TestReconcile_CredentialGatewayIgnoresVaultAgentReadiness(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		ready      map[string]bool
		wantStatus string
		wantOutput string
	}{
		"Vault Agent restarting keeps the gateway passing": {
			ready: map[string]bool{
				credentialGatewayEnvoyContainer:             true,
				constants.CredentialVaultAgentContainerName: false,
				constants.CredentialProcessorContainerName:  true,
			},
			wantStatus: api.HealthPassing,
			wantOutput: constants.KubernetesSuccessReasonMsg,
		},
		"processor not ready withdraws the gateway": {
			ready: map[string]bool{
				credentialGatewayEnvoyContainer:             true,
				constants.CredentialVaultAgentContainerName: true,
				constants.CredentialProcessorContainerName:  false,
			},
			wantStatus: api.HealthCritical,
			wantOutput: `Pod "default/terminating-gateway" is not ready`,
		},
		"Envoy not ready withdraws the gateway": {
			ready: map[string]bool{
				credentialGatewayEnvoyContainer:             false,
				constants.CredentialVaultAgentContainerName: false,
				constants.CredentialProcessorContainerName:  true,
			},
			wantStatus: api.HealthCritical,
			wantOutput: `Pod "default/terminating-gateway" is not ready`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pod := credentialGatewayPod(tc.ready)
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "terminating-gateway",
					Namespace: "default",
					Labels:    map[string]string{discoveryv1.LabelServiceName: "terminating-gateway"},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{
					Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(pod.Status.Conditions[0].Status == corev1.ConditionTrue)},
					Addresses:  []string{"1.2.3.4"},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace},
				}},
			}
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().
				WithScheme(endpointSliceTestScheme(t)).
				WithRuntimeObjects(pod, endpointSlice, &ns, &node).
				Build()
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			controller := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consulServer",
				ReleaseNamespace:      "default",
			}

			_, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "default", Name: "terminating-gateway"},
			})
			require.NoError(t, err)

			checks, _, err := testClient.APIClient.Health().Checks("terminating-gateway",
				&api.QueryOptions{Filter: fmt.Sprintf("ServiceID == %q", "terminating-gateway")})
			require.NoError(t, err)
			require.Len(t, checks, 1)
			require.Equal(t, constants.ConsulKubernetesCheckName, checks[0].Name)
			require.Equal(t, tc.wantStatus, checks[0].Status)
			require.Equal(t, tc.wantOutput, checks[0].Output)
		})
	}
}

// Without a Pod watch, a container readiness change that does not flip pod
// readiness (for example the processor failing while the Vault Agent is
// already restarting) would leave a stale health check in Consul.
func TestRequestsForCredentialGatewayPod(t *testing.T) {
	gatewaySlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "terminating-gateway-abcde",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "terminating-gateway"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"1.2.3.4"},
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "terminating-gateway", Namespace: "default"},
		}},
	}
	otherSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-abcde",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "other"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"1.2.3.5"},
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "other", Namespace: "default"},
		}},
	}
	controller := &Controller{
		Client: fake.NewClientBuilder().
			WithScheme(endpointSliceTestScheme(t)).
			WithRuntimeObjects(gatewaySlice, otherSlice).
			Build(),
		Log: logrtest.New(t),
	}

	requests := controller.requestsForCredentialGatewayPod(context.Background(),
		credentialGatewayPod(allCredentialGatewayContainersReady()))
	require.Equal(t, []reconcile.Request{{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "terminating-gateway"},
	}}, requests)

	plainGateway := createGatewayPod("terminating-gateway", "1.2.3.4", map[string]string{
		constants.AnnotationGatewayKind: terminatingGateway,
	})
	require.Empty(t, controller.requestsForCredentialGatewayPod(context.Background(), plainGateway))
	require.Empty(t, controller.requestsForCredentialGatewayPod(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: constants.CredentialVaultAgentContainerName}}},
	}))
}
