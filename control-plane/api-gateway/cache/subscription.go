// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"context"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/translation"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// Subscription represents a watcher for events on a specific kind.
type Subscription struct {
	translator translation.TranslatorFn
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
