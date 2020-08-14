/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers_test

import (
	"context"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/controllers"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestServiceDefaultsController_createsConfigEntry(t *testing.T) {
	req := require.New(t)
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}
	s := scheme.Scheme
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
	ctx := context.Background()

	consul, err := testutil.NewTestServerConfigT(t, nil)
	req.NoError(err)
	defer consul.Stop()
	consulClient, err := capi.NewClient(&capi.Config{
		Address: consul.HTTPAddr,
	})
	req.NoError(err)

	client := fake.NewFakeClientWithScheme(s, svcDefaults)

	r := controllers.ServiceDefaultsReconciler{
		Client:       client,
		Log:          logrtest.NullLogger{},
		Scheme:       s,
		ConsulClient: consulClient,
	}

	resp, err := r.Reconcile(ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: svcDefaults.ObjectMeta.Namespace,
			Name:      svcDefaults.ObjectMeta.Name,
		},
	})
	req.NoError(err)
	req.False(resp.Requeue)

	cfg, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, "foo", nil)
	req.NoError(err)
	svcDefault, ok := cfg.(*capi.ServiceConfigEntry)
	req.True(ok)
	req.Equal("http", svcDefault.Protocol)

	// Check that the status is "synced".
	err = client.Get(ctx, types.NamespacedName{
		Namespace: svcDefaults.Namespace,
		Name:      svcDefaults.Name,
	}, svcDefaults)
	req.NoError(err)
	conditionSynced := svcDefaults.Status.GetCondition(v1alpha1.ConditionSynced)
	req.True(conditionSynced.IsTrue())
}

func TestServiceDefaultsController_addsFinalizerOnCreate(t *testing.T) {
	req := require.New(t)
	s := scheme.Scheme
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)

	consul, err := testutil.NewTestServerConfigT(t, nil)
	req.NoError(err)
	defer consul.Stop()
	consulClient, err := capi.NewClient(&capi.Config{
		Address: consul.HTTPAddr,
	})
	req.NoError(err)

	client := fake.NewFakeClientWithScheme(s, svcDefaults)

	r := controllers.ServiceDefaultsReconciler{
		Client:       client,
		Log:          logrtest.NullLogger{},
		Scheme:       s,
		ConsulClient: consulClient,
	}

	resp, err := r.Reconcile(ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: svcDefaults.ObjectMeta.Namespace,
			Name:      svcDefaults.ObjectMeta.Name,
		},
	})
	req.NoError(err)
	req.False(resp.Requeue)

	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: svcDefaults.Namespace,
		Name:      svcDefaults.Name,
	}, svcDefaults)
	req.NoError(err)
	req.Contains(svcDefaults.Finalizers, controllers.FinalizerName)
	conditionSynced := svcDefaults.Status.GetCondition(v1alpha1.ConditionSynced)
	req.True(conditionSynced.IsTrue())
}

func TestServiceDefaultsController_updatesConfigEntry(t *testing.T) {
	req := require.New(t)
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "foo",
			Namespace:  "default",
			Finalizers: []string{controllers.FinalizerName},
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}
	s := scheme.Scheme
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
	ctx := context.Background()

	consul, err := testutil.NewTestServerConfigT(t, nil)
	req.NoError(err)
	defer consul.Stop()
	consulClient, err := capi.NewClient(&capi.Config{
		Address: consul.HTTPAddr,
	})
	req.NoError(err)

	client := fake.NewFakeClientWithScheme(s, svcDefaults)

	r := controllers.ServiceDefaultsReconciler{
		Client:       client,
		Log:          logrtest.NullLogger{},
		Scheme:       s,
		ConsulClient: consulClient,
	}

	// We haven't run reconcile yet so ensure it's created in Consul.
	{
		written, _, err := consulClient.ConfigEntries().Set(&capi.ServiceConfigEntry{
			Kind:     capi.ServiceDefaults,
			Name:     "foo",
			Protocol: "http",
		}, nil)
		req.NoError(err)
		req.True(written)
	}

	// Now update it.
	{
		// First get it so we have the latest revision number.
		err = client.Get(ctx, types.NamespacedName{
			Namespace: svcDefaults.Namespace,
			Name:      svcDefaults.Name,
		}, svcDefaults)
		req.NoError(err)

		// Update the protocol.
		svcDefaults.Spec.Protocol = "tcp"
		err := client.Update(ctx, svcDefaults)
		req.NoError(err)

		resp, err := r.Reconcile(ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: svcDefaults.ObjectMeta.Namespace,
				Name:      svcDefaults.ObjectMeta.Name,
			},
		})
		req.NoError(err)
		req.False(resp.Requeue)

		cfg, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, "foo", nil)
		req.NoError(err)
		svcDefault, ok := cfg.(*capi.ServiceConfigEntry)
		req.True(ok)
		req.Equal("tcp", svcDefault.Protocol)
	}
}

func TestServiceDefaultsController_deletesConfigEntry(t *testing.T) {
	req := require.New(t)
	// Create it with the deletion timestamp set to mimic that it's already
	// been marked for deletion.
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foo",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{controllers.FinalizerName},
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}
	s := scheme.Scheme
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)

	consul, err := testutil.NewTestServerConfigT(t, nil)
	req.NoError(err)
	defer consul.Stop()
	consulClient, err := capi.NewClient(&capi.Config{
		Address: consul.HTTPAddr,
	})
	req.NoError(err)

	client := fake.NewFakeClientWithScheme(s, svcDefaults)

	r := controllers.ServiceDefaultsReconciler{
		Client:       client,
		Log:          logrtest.NullLogger{},
		Scheme:       s,
		ConsulClient: consulClient,
	}

	// We haven't run reconcile yet so ensure it's created in Consul.
	{
		written, _, err := consulClient.ConfigEntries().Set(&capi.ServiceConfigEntry{
			Kind:     capi.ServiceDefaults,
			Name:     "foo",
			Protocol: "http",
		}, nil)
		req.NoError(err)
		req.True(written)
	}

	// Now run reconcile. It's marked for deletion so this should delete it.
	{
		resp, err := r.Reconcile(ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: svcDefaults.ObjectMeta.Namespace,
				Name:      svcDefaults.ObjectMeta.Name,
			},
		})
		req.NoError(err)
		req.False(resp.Requeue)

		_, _, err = consulClient.ConfigEntries().Get(capi.ServiceDefaults, "foo", nil)
		req.EqualError(err, "Unexpected response code: 404 (Config entry not found for \"service-defaults\" / \"foo\")")
	}
}

func TestServiceDefaultsController_errorUpdatesSyncStatus(t *testing.T) {
	req := require.New(t)
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}
	s := scheme.Scheme
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
	ctx := context.Background()

	consulClient, err := capi.NewClient(&capi.Config{
		Address: "incorrect-address",
	})
	req.NoError(err)

	client := fake.NewFakeClientWithScheme(s, svcDefaults)

	r := controllers.ServiceDefaultsReconciler{
		Client:       client,
		Log:          logrtest.NullLogger{},
		Scheme:       s,
		ConsulClient: consulClient,
	}

	resp, err := r.Reconcile(ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: svcDefaults.Namespace,
			Name:      svcDefaults.Name,
		},
	})
	req.EqualError(err, "Get \"http://incorrect-address/v1/config/service-defaults/foo\": dial tcp: lookup incorrect-address on 127.0.0.11:53: no such host")
	req.False(resp.Requeue)

	// Check that the status is "synced=false".
	err = client.Get(ctx, types.NamespacedName{
		Namespace: svcDefaults.Namespace,
		Name:      svcDefaults.Name,
	}, svcDefaults)
	req.NoError(err)
	conditionSynced := svcDefaults.Status.GetCondition(v1alpha1.ConditionSynced)
	req.True(conditionSynced.IsFalse())
	req.Equal("ConsulAgentError", conditionSynced.Reason)
	req.Equal("Get \"http://incorrect-address/v1/config/service-defaults/foo\": dial tcp: lookup incorrect-address on 127.0.0.11:53: no such host", conditionSynced.Message)
}
