// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/helper/controller"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	hclog.DefaultOptions.Level = hclog.Debug
}

func TestK8SSink_impl(t *testing.T) {
	var _ controller.Resource = &K8SSink{}
	var _ controller.Backgrounder = &K8SSink{}
	var _ Sink = &K8SSink{}
}

// Test that basic service creation works.
func TestK8SSink_create(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()

	// Start the controller
	sink, closer := testSink(t, client)
	defer closer()

	// Set a service
	sink.SetServices(map[string]string{"web": "web.service.local."})

	// Verify service gets registered
	var actual *apiv1.ServiceList
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		actual = list
	})

	found := false
	for _, s := range actual.Items {
		if s.Name == "web" {
			found = true
			break
		}
	}

	require.True(found, "found service")
}

// Test that we lowercase service names.
func TestK8SSink_createUppercase(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()

	// Start the controller
	sink, closer := testSink(t, client)
	defer closer()

	// Set a service
	sink.SetServices(map[string]string{"UPPERCASE": "UPPERCASE.service.local."})

	// Verify service gets registered
	var actual *apiv1.ServiceList
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		actual = list
	})

	found := false
	for _, s := range actual.Items {
		if s.Name == "uppercase" {
			found = true
			require.Equal("uppercase.service.local.", s.Spec.ExternalName)
			break
		}
	}

	require.True(found, "found service")
}

// Test that a service isn't registered if it exists already.
func TestK8SSink_createExists(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()

	// Create the existing service
	_, err := client.CoreV1().Services(metav1.NamespaceAll).Create(
		context.Background(),
		&apiv1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "web",
			},

			Spec: apiv1.ServiceSpec{
				Type:         apiv1.ServiceTypeExternalName,
				ExternalName: "example.com.",
			},
		},
		metav1.CreateOptions{})
	require.NoError(err)

	// Start the controller
	sink, closer := testSink(t, client)
	defer closer()

	// Set a service
	sink.SetServices(map[string]string{"web": "web.service.local."})

	// Verify service gets registered
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		var actual *apiv1.Service
		for _, s := range list.Items {
			if s.Name == "web" {
				actual = &s
				break
			}
		}

		if actual == nil {
			r.Fatal("web not found")
		} else if actual.Spec.ExternalName != "example.com." {
			r.Fatal("modified")
		}
	})
}

// Test that if the service is updated remotely, that we change it back.
func TestK8SSink_updateReconcile(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	client := fake.NewSimpleClientset()

	// Start the controller
	sink, closer := testSink(t, client)
	defer closer()

	// Set a service
	sink.SetServices(map[string]string{"web": "web.service.local."})

	// Verify service gets registered
	var actual *apiv1.Service
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		for _, s := range list.Items {
			if s.Name == "web" {
				actual = &s
				return
			}
		}

		r.Fatal("service not found")
	})

	actual.Spec.ExternalName = "wrong.local."
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Update(context.Background(), actual, metav1.UpdateOptions{})
	require.NoError(err)

	// Verify service gets fixed
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		for _, s := range list.Items {
			if s.Name == "web" {
				actual = &s
				break
			}
		}

		if actual == nil {
			r.Fatal("service not found")
		}

		if actual.Spec.ExternalName != "web.service.local." {
			r.Fatal("not updated")
		}
	})
}

// Test that if the service is updated locally, it is reconciled.
func TestK8SSink_updateService(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()

	// Start the controller
	sink, closer := testSink(t, client)
	defer closer()

	// Set a service
	sink.SetServices(map[string]string{"web": "web.service.local."})

	// Verify service gets registered
	var actual *apiv1.Service
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		for _, s := range list.Items {
			if s.Name == "web" {
				actual = &s
				return
			}
		}

		r.Fatal("service not found")
	})

	// Update a service
	sink.SetServices(map[string]string{"web": "web2.service.local."})

	// Verify service gets fixed
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		for _, s := range list.Items {
			if s.Name == "web" {
				actual = &s
				break
			}
		}

		if actual == nil {
			r.Fatal("service not found")
		}

		if actual.Spec.ExternalName != "web2.service.local." {
			r.Fatal("not updated")
		}
	})
}

// Test that if the service is deleted remotely, it is recreated.
func TestK8SSink_deleteReconcileRemote(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()

	// Start the controller
	sink, closer := testSink(t, client)
	defer closer()

	// Set a service
	sink.SetServices(map[string]string{"web": "web.service.local."})

	// Verify service gets registered
	var actual *apiv1.Service
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		for _, s := range list.Items {
			if s.Name == "web" {
				actual = &s
				return
			}
		}

		r.Fatal("service not found")
	})

	// Delete
	require.NoError(t, client.CoreV1().Services(metav1.NamespaceDefault).Delete(context.Background(), actual.Name, metav1.DeleteOptions{}))

	// Verify service gets fixed
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		for _, s := range list.Items {
			if s.Name == "web" {
				actual = &s
				break
			}
		}

		if actual == nil {
			r.Fatal("service not found")
		}

		if actual.Spec.ExternalName != "web.service.local." {
			r.Fatal("not updated")
		}
	})
}

// Test that if the service is deleted locally, it is recreated.
func TestK8SSink_deleteReconcileLocal(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()

	// Start the controller
	sink, closer := testSink(t, client)
	defer closer()

	// Set a service
	sink.SetServices(map[string]string{"web": "web.service.local."})

	// Verify service gets registered
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) == 0 {
			r.Fatal("no services")
		}

		for _, s := range list.Items {
			if s.Name == "web" {
				return
			}
		}

		r.Fatal("service not found")
	})

	// Clear
	sink.SetServices(map[string]string{})

	// Verify services get cleared
	retry.Run(t, func(r *retry.R) {
		list, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(list.Items) > 0 {
			r.Fatal("services")
		}
	})
}

func testSink(t *testing.T, client kubernetes.Interface) (*K8SSink, func()) {
	sink := &K8SSink{
		Client: client,
		Log:    hclog.Default(),
		Ctx:    context.Background(),
	}

	closer := controller.TestControllerRun(sink)
	return sink, closer
}
