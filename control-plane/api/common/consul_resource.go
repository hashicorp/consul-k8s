// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"github.com/hashicorp/consul/proto-public/pbresource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ConsulResource interface {
	ResourceID(namespace, partition string) *pbresource.ID
	Resource(namespace, partition string) *pbresource.Resource

	// GetObjectKind should be implemented by the generated code.
	GetObjectKind() schema.ObjectKind
	// DeepCopyObject should be implemented by the generated code.
	DeepCopyObject() runtime.Object

	// AddFinalizer adds a finalizer to the list of finalizers.
	AddFinalizer(name string)
	// RemoveFinalizer removes this finalizer from the list.
	RemoveFinalizer(name string)
	// Finalizers returns the list of finalizers for this object.
	Finalizers() []string

	// MatchesConsul returns true if the resource has the same fields as the Consul
	// config entry.
	MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool

	// KubeKind returns the Kube config entry kind, i.e. servicedefaults, not
	// service-defaults.
	KubeKind() string
	// KubernetesName returns the name of the Kubernetes resource.
	KubernetesName() string

	// SetSyncedCondition updates the synced condition.
	SetSyncedCondition(status corev1.ConditionStatus, reason, message string)
	// SetLastSyncedTime updates the last synced time.
	SetLastSyncedTime(time *metav1.Time)
	// SyncedCondition gets the synced condition.
	SyncedCondition() (status corev1.ConditionStatus, reason, message string)
	// SyncedConditionStatus returns the status of the synced condition.
	SyncedConditionStatus() corev1.ConditionStatus

	// Validate returns an error if the resource is invalid.
	Validate(tenancy ConsulTenancyConfig) error

	// DefaultNamespaceFields sets Consul namespace fields on the resource
	// spec to their default values if namespaces are enabled.
	DefaultNamespaceFields(tenancy ConsulTenancyConfig)

	// Object is required so that MeshConfig implements metav1.Object, which is
	// the interface supported by controller-runtime reconcile-able resources.
	metav1.Object
}
