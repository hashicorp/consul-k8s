// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strings"

	"github.com/hashicorp/consul/api"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func GatewayStatusesEqual(a, b gwv1beta1.GatewayStatus) bool {
	return slices.EqualFunc(a.Addresses, b.Addresses, gatewayStatusesAddressesEqual) &&
		slices.EqualFunc(a.Conditions, b.Conditions, conditionsEqual) &&
		slices.EqualFunc(a.Listeners, b.Listeners, gatewayStatusesListenersEqual)
}

func gatewayStatusesAddressesEqual(a, b gwv1beta1.GatewayAddress) bool {
	return BothNilOrEqual(a.Type, b.Type) &&
		a.Value == b.Value
}

func gatewayStatusesListenersEqual(a, b gwv1beta1.ListenerStatus) bool {
	return a.AttachedRoutes == b.AttachedRoutes &&
		a.Name == b.Name &&
		slices.EqualFunc(a.SupportedKinds, b.SupportedKinds, routeGroupKindsEqual) &&
		slices.EqualFunc(a.Conditions, b.Conditions, conditionsEqual)
}

func routeGroupKindsEqual(a, b gwv1beta1.RouteGroupKind) bool {
	return BothNilOrEqual(a.Group, b.Group) &&
		a.Kind == b.Kind
}

// this intentionally ignores the last set time so we don't
// always fail a conditional check per-reconciliation.
func conditionsEqual(a, b metav1.Condition) bool {
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message &&
		a.ObservedGeneration == b.ObservedGeneration
}

func EntriesEqual(a, b api.ConfigEntry) bool {
	switch aCast := a.(type) {
	case *api.APIGatewayConfigEntry:
		if bCast, ok := b.(*api.APIGatewayConfigEntry); ok {
			return apiGatewaysEqual(aCast, bCast)
		}
	case *api.HTTPRouteConfigEntry:
		if bCast, ok := b.(*api.HTTPRouteConfigEntry); ok {
			return httpRoutesEqual(aCast, bCast)
		}
	case *api.TCPRouteConfigEntry:
		if bCast, ok := b.(*api.TCPRouteConfigEntry); ok {
			return tcpRoutesEqual(aCast, bCast)
		}
	case *api.InlineCertificateConfigEntry:
		if bCast, ok := b.(*api.InlineCertificateConfigEntry); ok {
			return certificatesEqual(aCast, bCast)
		}
	}
	return false
}

type entryComparator struct {
	namespaceA string
	partitionA string
	namespaceB string
	partitionB string
}

func apiGatewaysEqual(a, b *api.APIGatewayConfigEntry) bool {
	if a == nil || b == nil {
		return false
	}

	return (entryComparator{
		namespaceA: NormalizeEmptyMetadataString(a.Namespace),
		partitionA: NormalizeEmptyMetadataString(a.Partition),
		namespaceB: NormalizeEmptyMetadataString(b.Namespace),
		partitionB: NormalizeEmptyMetadataString(b.Partition),
	}).apiGatewaysEqual(*a, *b)
}

func (e entryComparator) apiGatewaysEqual(a, b api.APIGatewayConfigEntry) bool {
	return a.Kind == b.Kind &&
		a.Name == b.Name &&
		e.namespaceA == e.namespaceB &&
		e.partitionA == e.partitionB &&
		maps.Equal(a.Meta, b.Meta) &&
		slices.EqualFunc(a.Listeners, b.Listeners, e.apiGatewayListenersEqual)
}

func (e entryComparator) apiGatewayListenersEqual(a, b api.APIGatewayListener) bool {
	return a.Hostname == b.Hostname &&
		a.Name == b.Name &&
		a.Port == b.Port &&
		// normalize the protocol name
		strings.EqualFold(a.Protocol, b.Protocol) &&
		e.apiGatewayListenerTLSConfigurationsEqual(a.TLS, b.TLS)
}

func (e entryComparator) apiGatewayListenerTLSConfigurationsEqual(a, b api.APIGatewayTLSConfiguration) bool {
	return a.MaxVersion == b.MaxVersion &&
		a.MinVersion == b.MinVersion &&
		slices.Equal(a.CipherSuites, b.CipherSuites) &&
		slices.EqualFunc(a.Certificates, b.Certificates, e.resourceReferencesEqual)
}

func (e entryComparator) resourceReferencesEqual(a, b api.ResourceReference) bool {
	return a.Kind == b.Kind &&
		a.Name == b.Name &&
		a.SectionName == b.SectionName &&
		orDefault(a.Namespace, e.namespaceA) == orDefault(b.Namespace, e.namespaceB) &&
		orDefault(a.Partition, e.partitionA) == orDefault(b.Partition, e.partitionB)
}

func httpRoutesEqual(a, b *api.HTTPRouteConfigEntry) bool {
	if a == nil || b == nil {
		return false
	}

	return (entryComparator{
		namespaceA: NormalizeEmptyMetadataString(a.Namespace),
		partitionA: NormalizeEmptyMetadataString(a.Partition),
		namespaceB: NormalizeEmptyMetadataString(b.Namespace),
		partitionB: NormalizeEmptyMetadataString(b.Partition),
	}).httpRoutesEqual(*a, *b)
}

func (e entryComparator) httpRoutesEqual(a, b api.HTTPRouteConfigEntry) bool {
	return a.Kind == b.Kind &&
		a.Name == b.Name &&
		e.namespaceA == e.namespaceB &&
		e.partitionA == e.partitionB &&
		maps.Equal(a.Meta, b.Meta) &&
		slices.Equal(a.Hostnames, b.Hostnames) &&
		slices.EqualFunc(a.Parents, b.Parents, e.resourceReferencesEqual) &&
		slices.EqualFunc(a.Rules, b.Rules, e.httpRouteRulesEqual)
}

func (e entryComparator) httpRouteRulesEqual(a, b api.HTTPRouteRule) bool {
	return slices.EqualFunc(a.Filters.Headers, b.Filters.Headers, e.httpHeaderFiltersEqual) &&
		bothNilOrEqualFunc(a.Filters.URLRewrite, b.Filters.URLRewrite, e.urlRewritesEqual) &&
		slices.EqualFunc(a.Matches, b.Matches, e.httpMatchesEqual) &&
		slices.EqualFunc(a.Services, b.Services, e.httpServicesEqual)
}

func (e entryComparator) httpServicesEqual(a, b api.HTTPService) bool {
	return a.Name == b.Name &&
		a.Weight == b.Weight &&
		orDefault(a.Namespace, e.namespaceA) == orDefault(b.Namespace, e.namespaceB) &&
		orDefault(a.Partition, e.partitionA) == orDefault(b.Partition, e.partitionB) &&
		slices.EqualFunc(a.Filters.Headers, b.Filters.Headers, e.httpHeaderFiltersEqual) &&
		bothNilOrEqualFunc(a.Filters.URLRewrite, b.Filters.URLRewrite, e.urlRewritesEqual)
}

func (e entryComparator) httpMatchesEqual(a, b api.HTTPMatch) bool {
	return a.Method == b.Method &&
		slices.EqualFunc(a.Headers, b.Headers, e.httpHeaderMatchesEqual) &&
		slices.EqualFunc(a.Query, b.Query, e.httpQueryMatchesEqual) &&
		e.httpPathMatchesEqual(a.Path, b.Path)
}

func (e entryComparator) httpPathMatchesEqual(a, b api.HTTPPathMatch) bool {
	return a.Match == b.Match && a.Value == b.Value
}

func (e entryComparator) httpHeaderMatchesEqual(a, b api.HTTPHeaderMatch) bool {
	return a.Match == b.Match && a.Name == b.Name && a.Value == b.Value
}

func (e entryComparator) httpQueryMatchesEqual(a, b api.HTTPQueryMatch) bool {
	return a.Match == b.Match && a.Name == b.Name && a.Value == b.Value
}

func (e entryComparator) httpHeaderFiltersEqual(a, b api.HTTPHeaderFilter) bool {
	return maps.Equal(a.Add, b.Add) &&
		maps.Equal(a.Set, b.Set) &&
		slices.Equal(a.Remove, b.Remove)
}

func (e entryComparator) urlRewritesEqual(a, b api.URLRewrite) bool {
	return a.Path == b.Path
}

func tcpRoutesEqual(a, b *api.TCPRouteConfigEntry) bool {
	if a == nil || b == nil {
		return false
	}

	return (entryComparator{
		namespaceA: NormalizeEmptyMetadataString(a.Namespace),
		partitionA: NormalizeEmptyMetadataString(a.Partition),
		namespaceB: NormalizeEmptyMetadataString(b.Namespace),
		partitionB: NormalizeEmptyMetadataString(b.Partition),
	}).tcpRoutesEqual(*a, *b)
}

func (e entryComparator) tcpRoutesEqual(a, b api.TCPRouteConfigEntry) bool {
	return a.Kind == b.Kind &&
		a.Name == b.Name &&
		e.namespaceA == e.namespaceB &&
		e.partitionA == e.partitionB &&
		maps.Equal(a.Meta, b.Meta) &&
		slices.EqualFunc(a.Parents, b.Parents, e.resourceReferencesEqual) &&
		slices.EqualFunc(a.Services, b.Services, e.tcpRouteServicesEqual)
}

func (e entryComparator) tcpRouteServicesEqual(a, b api.TCPService) bool {
	return a.Name == b.Name &&
		orDefault(a.Namespace, e.namespaceA) == orDefault(b.Namespace, e.namespaceB) &&
		orDefault(a.Partition, e.partitionA) == orDefault(b.Partition, e.partitionB)
}

func certificatesEqual(a, b *api.InlineCertificateConfigEntry) bool {
	if a == nil || b == nil {
		return false
	}

	return (entryComparator{
		namespaceA: NormalizeEmptyMetadataString(a.Namespace),
		partitionA: NormalizeEmptyMetadataString(a.Partition),
		namespaceB: NormalizeEmptyMetadataString(b.Namespace),
		partitionB: NormalizeEmptyMetadataString(b.Partition),
	}).certificatesEqual(*a, *b)
}

func (e entryComparator) certificatesEqual(a, b api.InlineCertificateConfigEntry) bool {
	return a.Kind == b.Kind &&
		a.Name == b.Name &&
		e.namespaceA == e.namespaceB &&
		e.partitionA == e.partitionB &&
		maps.Equal(a.Meta, b.Meta) &&
		a.Certificate == b.Certificate &&
		a.PrivateKey == b.PrivateKey
}

func bothNilOrEqualFunc[T any](one, two *T, fn func(T, T) bool) bool {
	if one == nil && two == nil {
		return true
	}
	if one == nil {
		return false
	}
	if two == nil {
		return false
	}
	return fn(*one, *two)
}

func orDefault[T ~string](v T, fallback string) string {
	if v == "" {
		return fallback
	}
	return string(v)
}
