// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestMcpServerConfigReconcile(t *testing.T) {
	t.Parallel()
	deletionTimestamp := metav1.Now()

	cases := []struct {
		name         string
		k8sObjects   func() []runtime.Object
		expErr       string
		requeue      bool
		requeueAfter time.Duration
	}{
		{
			name: "resource not found returns no error",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{}
			},
		},
		{
			name: "new enabled resource gets finalizer and Ready=True",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{enabledMcp("consul-mcp-server")}
			},
		},
		{
			name: "disabled resource gets Ready=False",
			k8sObjects: func() []runtime.Object {
				mcp := enabledMcp("consul-mcp-server")
				mcp.Spec.Enabled = false
				return []runtime.Object{mcp}
			},
		},
		{
			name: "resource marked for deletion — finalizer is removed",
			k8sObjects: func() []runtime.Object {
				mcp := enabledMcp("consul-mcp-server")
				mcp.ObjectMeta.DeletionTimestamp = &deletionTimestamp
				mcp.ObjectMeta.Finalizers = []string{mcpServerConfigFinalizer}
				return []runtime.Object{mcp}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.k8sObjects()...).
				WithStatusSubresource(&v1alpha1.McpServerConfig{}).
				Build()

			controller := &McpServerConfigController{
				Client: fakeClient,
				Log:    logrtest.New(t),
			}

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "consul-mcp-server"},
			})

			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.requeue, resp.Requeue)
		})
	}
}

func enabledMcp(name string) *v1alpha1.McpServerConfig {
	return &v1alpha1.McpServerConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.McpServerConfigSpec{
			Enabled: true,
			Defaults: v1alpha1.McpServerDefaults{
				InterceptorPort: 21101,
				Transport:       "streamable-http",
				Path:            "/mcp",
				ProtocolVersion: "2025-03-26",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("128Mi"),
						corev1.ResourceCPU:    resource.MustParse("250m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
						corev1.ResourceCPU:    resource.MustParse("500m"),
					},
				},
			},
		},
	}
}
