// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	kubernetestesting "k8s.io/client-go/testing"
)

func TestOpenShiftStaticWorkloadFixture(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    string
		wantCNI string
	}{
		{fixture: "../fixtures/cases/static-server-inject", want: "../fixtures/cases/static-server-openshift", wantCNI: "../fixtures/cases/static-server-openshift-cni"},
		{fixture: "../fixtures/cases/static-client-inject", want: "../fixtures/cases/static-client-openshift-inject", wantCNI: "../fixtures/cases/static-client-openshift-inject-cni"},
		{fixture: "../fixtures/cases/static-client-tproxy", want: "../fixtures/cases/static-client-openshift-tproxy", wantCNI: "../fixtures/cases/static-client-openshift-tproxy-cni"},
		{fixture: "../fixtures/bases/static-server-tcp", want: "../fixtures/bases/static-server-tcp", wantCNI: "../fixtures/cases/static-server-tcp-openshift-cni"},
		{fixture: "../fixtures/cases/static-client-namespaces", want: "../fixtures/cases/static-client-namespaces", wantCNI: "../fixtures/cases/static-client-namespaces"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			require.Equal(t, tc.want, openShiftStaticWorkloadFixture(tc.fixture, false))
			require.Equal(t, tc.wantCNI, openShiftStaticWorkloadFixture(tc.fixture, true))
			require.DirExists(t, "../../tests/"+tc.want[len("../"):])
			require.DirExists(t, "../../tests/"+tc.wantCNI[len("../"):])
		})
	}
}

func TestEnsureOpenShiftNamespace(t *testing.T) {
	t.Run("creates a missing namespace", func(t *testing.T) {
		client := kubernetesfake.NewClientset()
		require.True(t, EnsureOpenShiftNamespace(t, client, "ns1"))
		ns, err := client.CoreV1().Namespaces().Get(context.Background(), "ns1", metav1.GetOptions{})
		require.NoError(t, err)
		require.Nil(t, ns.DeletionTimestamp)
	})
	t.Run("does not claim an active namespace", func(t *testing.T) {
		client := kubernetesfake.NewClientset(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{"owner": "another-test"}},
		})
		require.False(t, EnsureOpenShiftNamespace(t, client, "ns1"))
		ns, err := client.CoreV1().Namespaces().Get(context.Background(), "ns1", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "another-test", ns.Labels["owner"])
	})
	t.Run("waits for a terminating namespace before creating", func(t *testing.T) {
		client := kubernetesfake.NewClientset()
		gets := 0
		client.PrependReactor("get", "namespaces", func(kubernetestesting.Action) (bool, runtime.Object, error) {
			gets++
			if gets == 1 {
				now := metav1.Now()
				return true, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1", DeletionTimestamp: &now}}, nil
			}

			return false, nil, nil
		})
		require.True(t, EnsureOpenShiftNamespace(t, client, "ns1"))
		require.GreaterOrEqual(t, gets, 2)
	})
	t.Run("rechecks a concurrent create instead of assuming active", func(t *testing.T) {
		client := kubernetesfake.NewClientset()
		creates := 0
		client.PrependReactor("create", "namespaces", func(kubernetestesting.Action) (bool, runtime.Object, error) {
			creates++
			if creates == 1 {
				return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "namespaces"}, "ns1")
			}
			return false, nil, nil
		})
		require.True(t, EnsureOpenShiftNamespace(t, client, "ns1"))
		require.Equal(t, 2, creates)
	})
	for _, transient := range []error{
		apierrors.NewServerTimeout(schema.GroupResource{Resource: "namespaces"}, "get", 1),
		errors.New("net/http: TLS handshake timeout"),
	} {
		t.Run("retries "+transient.Error(), func(t *testing.T) {
			client := kubernetesfake.NewClientset()
			gets := 0
			client.PrependReactor("get", "namespaces", func(kubernetestesting.Action) (bool, runtime.Object, error) {
				gets++
				if gets == 1 {
					return true, nil, transient
				}
				return false, nil, nil
			})
			require.True(t, EnsureOpenShiftNamespace(t, client, "ns1"))
			require.Equal(t, 2, gets)
		})
	}
	t.Run("retries a transient namespace create failure", func(t *testing.T) {
		client := kubernetesfake.NewClientset()
		creates := 0
		client.PrependReactor("create", "namespaces", func(kubernetestesting.Action) (bool, runtime.Object, error) {
			creates++
			if creates == 1 {
				return true, nil, apierrors.NewServerTimeout(schema.GroupResource{Resource: "namespaces"}, "create", 1)
			}
			return false, nil, nil
		})
		require.True(t, EnsureOpenShiftNamespace(t, client, "ns1"))
		require.Equal(t, 2, creates)
	})
	t.Run("reports API errors without retrying", func(t *testing.T) {
		client := kubernetesfake.NewClientset()
		client.PrependReactor("get", "namespaces", func(kubernetestesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "ns1", errors.New("denied"))
		})
		_, err := ensureOpenShiftNamespace(context.Background(), client, "ns1")
		require.True(t, apierrors.IsForbidden(err))
		require.Len(t, client.Actions(), 1)
	})
	t.Run("reports the conditions of a stuck namespace", func(t *testing.T) {
		now := metav1.Now()
		client := kubernetesfake.NewClientset(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "ns1", DeletionTimestamp: &now},
			Status: corev1.NamespaceStatus{Conditions: []corev1.NamespaceCondition{{
				Type: corev1.NamespaceContentRemaining, Status: corev1.ConditionTrue, Message: "HTTPRoutes still have finalizers",
			}}},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := ensureOpenShiftNamespace(ctx, client, "ns1")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, "HTTPRoutes still have finalizers")
	})
}

func TestIsRetryableOpenShiftAPIError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{
			name: "observed wrapped discovery timeout",
			err: fmt.Errorf("failed to get server groups: %w", &url.Error{
				Op: "Get", URL: "https://api.example.invalid/api",
				Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ETIMEDOUT},
			}),
			want: true,
		},
		{name: "connection reset", err: fmt.Errorf("request failed: %w", syscall.ECONNRESET), want: true},
		{name: "observed CLI timeout", err: errors.New("read tcp: read: connection timed out"), want: true},
		{name: "TLS handshake timeout", err: errors.New("net/http: TLS handshake timeout"), want: true},
		{name: "unauthorized", err: apierrors.NewUnauthorized("login expired")},
		{name: "ownership conflict", err: errors.New("CRD exists with invalid ownership metadata")},
		{name: "invalid input", err: errors.New("invalid namespace name")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsRetryableOpenShiftAPIError(tc.err))
		})
	}
}
