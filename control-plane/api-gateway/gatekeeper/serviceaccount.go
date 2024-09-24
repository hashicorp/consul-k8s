// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

func (g *Gatekeeper) upsertServiceAccount(ctx context.Context, gateway gwv1beta1.Gateway, config common.HelmConfig) error {
	if config.AuthMethod == "" && !config.EnableOpenShift && len(config.ImagePullSecrets) == 0 {
		return g.deleteServiceAccount(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name})
	}

	serviceAccount := &corev1.ServiceAccount{}
	exists := false

	// Get ServiceAccount if it exists.
	err := g.Client.Get(ctx, g.namespacedName(gateway), serviceAccount)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if k8serrors.IsNotFound(err) {
		exists = false
	} else {
		exists = true
	}

	if exists {
		// Ensure we own the ServiceAccount.
		for _, ref := range serviceAccount.GetOwnerReferences() {
			if ref.UID == gateway.GetUID() && ref.Name == gateway.GetName() {
				// We found ourselves!
				return nil
			}
		}
		return errors.New("ServiceAccount not owned by controller")
	}

	// Create the ServiceAccount.
	serviceAccount = g.serviceAccount(gateway, config)
	if err := ctrl.SetControllerReference(&gateway, serviceAccount, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, serviceAccount); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deleteServiceAccount(ctx context.Context, gwName types.NamespacedName) error {
	if err := g.Client.Delete(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: gwName.Name, Namespace: gwName.Namespace}}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) serviceAccount(gateway gwv1beta1.Gateway, config common.HelmConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    common.LabelsForGateway(&gateway),
		},
		ImagePullSecrets: config.ImagePullSecrets,
	}
}
