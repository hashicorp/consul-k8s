// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"context"

	capi "github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// TranslatorFn maps a changed Consul ai-gateway config entry to the K8s
// NamespacedName(s) of the InferenceGateway object(s) that should be
// re-queued for reconciliation.
//
// Mirrors api-gateway/cache/subscription.go:TranslatorFn.
type TranslatorFn func(capi.ConfigEntry) []types.NamespacedName

// Subscription represents a watcher for Consul ai-gateway config entry events.
// The Events() channel delivers one GenericEvent per changed entry, with the
// Object carrying the K8s name/namespace returned by the TranslatorFn.
//
// Mirrors api-gateway/cache/subscription.go:Subscription.
type Subscription struct {
	translator TranslatorFn
	ctx        context.Context
	cancelCtx  context.CancelFunc
	events     chan event.GenericEvent
}

// Cancel stops the subscription. The Events() channel is closed and no further
// events are delivered.
func (s *Subscription) Cancel() {
	s.cancelCtx()
}

// Events returns the channel on which GenericEvents are delivered.
// Wire this into a controller-runtime builder with WatchesRawSource +
// source.Channel, exactly as api-gateway/controllers/gateway_controller.go does.
func (s *Subscription) Events() chan event.GenericEvent {
	return s.events
}
