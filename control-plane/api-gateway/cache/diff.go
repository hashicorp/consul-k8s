// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"strings"

	"github.com/hashicorp/consul/api"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

func entriesEqual(a, b api.ConfigEntry) bool {
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
		namespaceA: normalizeEmptyMetadataString(a.Namespace),
		partitionA: normalizeEmptyMetadataString(a.Partition),
		namespaceB: normalizeEmptyMetadataString(b.Namespace),
		partitionB: normalizeEmptyMetadataString(b.Partition),
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
		strings.ToLower(a.Protocol) == strings.ToLower(b.Protocol) &&
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
		namespaceA: normalizeEmptyMetadataString(a.Namespace),
		partitionA: normalizeEmptyMetadataString(a.Partition),
		namespaceB: normalizeEmptyMetadataString(b.Namespace),
		partitionB: normalizeEmptyMetadataString(b.Partition),
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
		namespaceA: normalizeEmptyMetadataString(a.Namespace),
		partitionA: normalizeEmptyMetadataString(a.Partition),
		namespaceB: normalizeEmptyMetadataString(b.Namespace),
		partitionB: normalizeEmptyMetadataString(b.Partition),
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
		namespaceA: normalizeEmptyMetadataString(a.Namespace),
		partitionA: normalizeEmptyMetadataString(a.Partition),
		namespaceB: normalizeEmptyMetadataString(b.Namespace),
		partitionB: normalizeEmptyMetadataString(b.Partition),
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

func bothNilOrEqual[T comparable](one, two *T) bool {
	if one == nil && two == nil {
		return true
	}
	if one == nil {
		return false
	}
	if two == nil {
		return false
	}
	return *one == *two
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
