// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestAIConfigFromAgentCRD(t *testing.T) {
	defaults := v1alpha1.AgentDefaults{
		InterceptorPort: 22001, McpPort: 22002,
		HITL: v1alpha1.AgentHITL{Port: 22003, ApprovalTimeout: "60s"},
	}
	for _, tc := range []struct {
		name         string
		configName   string
		annotations  map[string]string
		zeroDefaults bool
		wantPorts    []int
		wantTimeout  string
	}{
		{name: "CRD defaults", wantPorts: []int{22001, 22002, 22003}, wantTimeout: "60s"},
		{name: "all overrides with selected CRD", configName: "custom",
			annotations: map[string]string{
				constants.AnnotationAIAgentConfig:              "custom",
				constants.AnnotationAIAgentInterceptorPort:     "23001",
				constants.AnnotationAIAgentMCPPort:             "23002",
				constants.AnnotationAIAgentHITLPort:            "23003",
				constants.AnnotationAIAgentHITLApprovalTimeout: "90s",
			}, wantPorts: []int{23001, 23002, 23003}, wantTimeout: "90s"},
		{name: "partial override", annotations: map[string]string{constants.AnnotationAIAgentMCPPort: "23002"},
			wantPorts: []int{22001, 23002, 22003}, wantTimeout: "60s"},
		{name: "empty overrides preserve defaults", annotations: map[string]string{
			constants.AnnotationAIAgentInterceptorPort: "", constants.AnnotationAIAgentMCPPort: "",
			constants.AnnotationAIAgentHITLPort: "", constants.AnnotationAIAgentHITLApprovalTimeout: "",
		}, wantPorts: []int{22001, 22002, 22003}, wantTimeout: "60s"},
		{name: "named port", annotations: map[string]string{constants.AnnotationAIAgentHITLPort: "approval"},
			wantPorts: []int{22001, 22002, 23003}, wantTimeout: "60s"},
		{name: "built-in ports", zeroDefaults: true,
			wantPorts: []int{constants.DefaultAIInterceptorPort, constants.DefaultAIMCPPort, constants.DefaultAIHITLPort}},
		{name: "override with zero defaults", zeroDefaults: true,
			annotations: map[string]string{constants.AnnotationAIAgentMCPPort: "23002"},
			wantPorts:   []int{constants.DefaultAIInterceptorPort, 23002, constants.DefaultAIHITLPort}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(s))
			name := tc.configName
			if name == "" {
				name = "consul-ai-agent"
			}
			cfg := &v1alpha1.AgentConfig{ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: v1alpha1.AgentConfigSpec{Defaults: defaults}}
			if tc.zeroDefaults {
				cfg.Spec.Defaults = v1alpha1.AgentDefaults{}
			}
			c := fake.NewClientBuilder().WithScheme(s).WithObjects(cfg).Build()
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tc.annotations},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "app", Ports: []corev1.ContainerPort{{Name: "approval", ContainerPort: 23003}},
				}}},
			}
			before := pod.DeepCopy()
			ai, err := AIConfigFromAgentCRD(context.Background(), c, pod)
			require.NoError(t, err)
			require.Equal(t, constants.AIAgentRole, ai.Role)
			require.Equal(t, tc.wantPorts, []int{ai.Agent.Interceptor.Port, ai.Agent.MCP.Port, ai.Agent.MCP.HITL.Port})
			require.Equal(t, tc.wantTimeout, ai.Agent.MCP.HITL.ApprovalTimeout)
			require.Equal(t, *before, pod)
			var stored v1alpha1.AgentConfig
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(cfg), &stored))
			require.Equal(t, cfg.Spec.Defaults, stored.Spec.Defaults, "pod overrides must not mutate the CRD")
		})
	}
}

func TestResolveAIAgentDefaults_InvalidPorts(t *testing.T) {
	for _, annotation := range []string{
		constants.AnnotationAIAgentInterceptorPort,
		constants.AnnotationAIAgentMCPPort,
		constants.AnnotationAIAgentHITLPort,
	} {
		for _, value := range []string{"invalid", "-1", "0", "1023", "65536"} {
			t.Run(annotation+"/"+value, func(t *testing.T) {
				pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{annotation: value}}}
				_, err := ResolveAIAgentDefaults(pod, v1alpha1.AgentDefaults{})
				require.ErrorContains(t, err, annotation)
			})
		}
	}
}

func TestAIConfigFromAgentCRD_MissingConfig(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(s))
	c := fake.NewClientBuilder().WithScheme(s).Build()
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		constants.AnnotationAIAgentConfig: "missing",
	}}}
	ai, err := AIConfigFromAgentCRD(context.Background(), c, pod)
	require.ErrorContains(t, err, "failed to get AgentConfig missing")
	require.Nil(t, ai)
}
