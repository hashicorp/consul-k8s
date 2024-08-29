// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

func (g *Gatekeeper) upsertSecret(ctx context.Context, gateway gwv1beta1.Gateway) error {
	desiredSecret, err := g.secret(ctx, gateway)
	if err != nil {
		return fmt.Errorf("failed to create certificate secret for gateway %s/%s: %w", gateway.Namespace, gateway.Name, err)
	}

	// If the Secret already exists, ensure that we own the Secret
	existingSecret := &corev1.Secret{ObjectMeta: desiredSecret.ObjectMeta}
	err = g.Client.Get(ctx, g.namespacedName(gateway), existingSecret)
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to fetch existing Secret %s/%s: %w", gateway.Namespace, gateway.Name, err)
	} else if !k8serrors.IsNotFound(err) {
		if !isOwnedByGateway(existingSecret, gateway) {
			return fmt.Errorf("existing Secret %s/%s is not owned by Gateway %s/%s", existingSecret.Namespace, existingSecret.Name, gateway.Namespace, gateway.Name)
		}
	}

	mutator := newSecretMutator(existingSecret, desiredSecret, gateway, g.Client.Scheme())

	result, err := controllerruntime.CreateOrUpdate(ctx, g.Client, existingSecret, mutator)
	if err != nil {
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		g.Log.V(1).Info("Created Secret")
	case controllerutil.OperationResultUpdated:
		g.Log.V(1).Info("Updated Secret")
	case controllerutil.OperationResultNone:
		g.Log.V(1).Info("No change to Secret")
	}

	return nil
}

func (g *Gatekeeper) deleteSecret(ctx context.Context, gw gwv1beta1.Gateway) error {
	secret := &corev1.Secret{}
	if err := g.Client.Get(ctx, g.namespacedName(gw), secret); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !isOwnedByGateway(secret, gw) {
		return fmt.Errorf("existing Secret %s/%s is not owned by Gateway %s/%s", secret.Namespace, secret.Name, gw.Namespace, gw.Name)
	}

	if err := g.Client.Delete(ctx, secret); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) secret(ctx context.Context, gateway gwv1beta1.Gateway) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gateway.Namespace,
			Name:      gateway.Name,
			Labels:    common.LabelsForGateway(&gateway),
		},
		Data: map[string][]byte{},
		Type: corev1.SecretTypeOpaque,
	}

	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil {
			continue
		}

		for _, ref := range listener.TLS.CertificateRefs {
			// Only take action on Secret references
			if !common.NilOrEqual(ref.Group, "") || !common.NilOrEqual(ref.Kind, common.KindSecret) {
				continue
			}

			key := types.NamespacedName{
				Namespace: common.ValueOr(ref.Namespace, gateway.Namespace),
				Name:      string(ref.Name),
			}

			referencedSecret := &corev1.Secret{}
			if err := g.Client.Get(ctx, key, referencedSecret); err != nil && k8serrors.IsNotFound(err) {
				// If the referenced Secret is not found, log a message and continue.
				// The issue will be raised on the Gateway status by the validation process.
				g.Log.V(1).Info(fmt.Sprintf("Referenced certificate secret %s/%s not found", key.Namespace, key.Name))
			} else if err != nil {
				return nil, fmt.Errorf("failed to fetch certificate secret %s/%s: %w", key.Namespace, key.Name, err)
			}

			prefix := fmt.Sprintf("%s_%s_", key.Namespace, key.Name)
			for k, v := range referencedSecret.Data {
				secret.Data[prefix+k] = v
			}
		}
	}

	return secret, nil
}

func newSecretMutator(existing, desired *corev1.Secret, gateway gwv1beta1.Gateway, scheme *runtime.Scheme) resourceMutator {
	return func() error {
		existing.Data = desired.Data
		return controllerruntime.SetControllerReference(&gateway, existing, scheme)
	}
}
