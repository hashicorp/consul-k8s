// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"context"

	"github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type TranslatorFn func(api.ConfigEntry) []types.NamespacedName

// Subscription represents a watcher for events on a specific kind.
type Subscription struct {
	translator TranslatorFn
	ctx        context.Context
	cancelCtx  context.CancelFunc
	events     chan event.GenericEvent
}

func (s *Subscription) Cancel() {
	s.cancelCtx()
}

func (s *Subscription) Events() chan event.GenericEvent {
	return s.events
}
