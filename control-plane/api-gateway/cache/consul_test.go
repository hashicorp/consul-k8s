// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/go-logr/logr"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func Test_resourceCache_diff(t *testing.T) {
	t.Parallel()
	type args struct {
		newCache *common.ReferenceMap
	}
	tests := []struct {
		name     string
		oldCache *common.ReferenceMap
		args     args
		want     []api.ConfigEntry
	}{
		{
			name: "no difference",
			oldCache: loadedReferenceMaps([]api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
			})[api.HTTPRoute],
			args: args{
				newCache: loadedReferenceMaps([]api.ConfigEntry{
					&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Namespace:   "ns",
							},
						},
						Rules: []api.HTTPRouteRule{
							{
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"add it on": "the value",
											},
											Remove: []string{"time to go"},
											Set: map[string]string{
												"Magic":       "v2",
												"Another One": "dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{Path: "v1"},
								},
								Matches: []api.HTTPMatch{
									{
										Headers: []api.HTTPHeaderMatch{
											{
												Match: api.HTTPHeaderMatchExact,
												Name:  "my header match",
												Value: "the value",
											},
										},
										Method: api.HTTPMatchMethodGet,
										Path: api.HTTPPathMatch{
											Match: api.HTTPPathMatchPrefix,
											Value: "/v1",
										},
										Query: []api.HTTPQueryMatch{
											{
												Match: api.HTTPQueryMatchExact,
												Name:  "search",
												Value: "term",
											},
										},
									},
								},
								Services: []api.HTTPService{
									{
										Name:   "service one",
										Weight: 45,
										Filters: api.HTTPFilters{
											Headers: []api.HTTPHeaderFilter{
												{
													Add: map[string]string{
														"svc - add it on": "svc - the value",
													},
													Remove: []string{"svc - time to go"},
													Set: map[string]string{
														"svc - Magic":       "svc - v2",
														"svc - Another One": "svc - dj khaled",
													},
												},
											},
											URLRewrite: &api.URLRewrite{
												Path: "path",
											},
										},
										Namespace: "some ns",
									},
								},
							},
						},
						Hostnames: []string{"hostname.com"},
						Meta: map[string]string{
							constants.MetaKeyKubeName: "name",
						},
						Status: api.ConfigEntryStatus{},
					},
				})[api.HTTPRoute],
			},
			want: []api.ConfigEntry{},
		},
		{
			name: "resource exists in old cache but not new one",
			oldCache: loadedReferenceMaps([]api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route 2",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-2",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
			})[api.HTTPRoute],
			args: args{
				newCache: loadedReferenceMaps([]api.ConfigEntry{
					&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Namespace:   "ns",
							},
						},
						Rules: []api.HTTPRouteRule{
							{
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"add it on": "the value",
											},
											Remove: []string{"time to go"},
											Set: map[string]string{
												"Magic":       "v2",
												"Another One": "dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{Path: "v1"},
								},
								Matches: []api.HTTPMatch{
									{
										Headers: []api.HTTPHeaderMatch{
											{
												Match: api.HTTPHeaderMatchExact,
												Name:  "my header match",
												Value: "the value",
											},
										},
										Method: api.HTTPMatchMethodGet,
										Path: api.HTTPPathMatch{
											Match: api.HTTPPathMatchPrefix,
											Value: "/v1",
										},
										Query: []api.HTTPQueryMatch{
											{
												Match: api.HTTPQueryMatchExact,
												Name:  "search",
												Value: "term",
											},
										},
									},
								},
								Services: []api.HTTPService{
									{
										Name:   "service one",
										Weight: 45,
										Filters: api.HTTPFilters{
											Headers: []api.HTTPHeaderFilter{
												{
													Add: map[string]string{
														"svc - add it on": "svc - the value",
													},
													Remove: []string{"svc - time to go"},
													Set: map[string]string{
														"svc - Magic":       "svc - v2",
														"svc - Another One": "svc - dj khaled",
													},
												},
											},
											URLRewrite: &api.URLRewrite{
												Path: "path",
											},
										},
										Namespace: "some ns",
									},
								},
							},
						},
						Hostnames: []string{"hostname.com"},
						Meta: map[string]string{
							constants.MetaKeyKubeName: "name",
						},
						Status: api.ConfigEntryStatus{},
					},
				})[api.HTTPRoute],
			},
			want: []api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route 2",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-2",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
			},
		},
		{
			name: "resource exists in new cache but not old one",
			oldCache: loadedReferenceMaps([]api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
			})[api.HTTPRoute],
			args: args{
				newCache: loadedReferenceMaps([]api.ConfigEntry{
					&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Namespace:   "ns",
							},
						},
						Rules: []api.HTTPRouteRule{
							{
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"add it on": "the value",
											},
											Remove: []string{"time to go"},
											Set: map[string]string{
												"Magic":       "v2",
												"Another One": "dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{Path: "v1"},
								},
								Matches: []api.HTTPMatch{
									{
										Headers: []api.HTTPHeaderMatch{
											{
												Match: api.HTTPHeaderMatchExact,
												Name:  "my header match",
												Value: "the value",
											},
										},
										Method: api.HTTPMatchMethodGet,
										Path: api.HTTPPathMatch{
											Match: api.HTTPPathMatchPrefix,
											Value: "/v1",
										},
										Query: []api.HTTPQueryMatch{
											{
												Match: api.HTTPQueryMatchExact,
												Name:  "search",
												Value: "term",
											},
										},
									},
								},
								Services: []api.HTTPService{
									{
										Name:   "service one",
										Weight: 45,
										Filters: api.HTTPFilters{
											Headers: []api.HTTPHeaderFilter{
												{
													Add: map[string]string{
														"svc - add it on": "svc - the value",
													},
													Remove: []string{"svc - time to go"},
													Set: map[string]string{
														"svc - Magic":       "svc - v2",
														"svc - Another One": "svc - dj khaled",
													},
												},
											},
											URLRewrite: &api.URLRewrite{
												Path: "path",
											},
										},
										Namespace: "some ns",
									},
								},
							},
						},
						Hostnames: []string{"hostname.com"},
						Meta: map[string]string{
							constants.MetaKeyKubeName: "name",
						},
						Status: api.ConfigEntryStatus{},
					},
					&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route 2",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-2",
								Namespace:   "ns",
							},
						},
						Rules: []api.HTTPRouteRule{
							{
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"add it on": "the value",
											},
											Remove: []string{"time to go"},
											Set: map[string]string{
												"Magic":       "v2",
												"Another One": "dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{Path: "v1"},
								},
								Matches: []api.HTTPMatch{
									{
										Headers: []api.HTTPHeaderMatch{
											{
												Match: api.HTTPHeaderMatchExact,
												Name:  "my header match",
												Value: "the value",
											},
										},
										Method: api.HTTPMatchMethodGet,
										Path: api.HTTPPathMatch{
											Match: api.HTTPPathMatchPrefix,
											Value: "/v1",
										},
										Query: []api.HTTPQueryMatch{
											{
												Match: api.HTTPQueryMatchExact,
												Name:  "search",
												Value: "term",
											},
										},
									},
								},
								Services: []api.HTTPService{
									{
										Name:   "service one",
										Weight: 45,
										Filters: api.HTTPFilters{
											Headers: []api.HTTPHeaderFilter{
												{
													Add: map[string]string{
														"svc - add it on": "svc - the value",
													},
													Remove: []string{"svc - time to go"},
													Set: map[string]string{
														"svc - Magic":       "svc - v2",
														"svc - Another One": "svc - dj khaled",
													},
												},
											},
											URLRewrite: &api.URLRewrite{
												Path: "path",
											},
										},
										Namespace: "some ns",
									},
								},
							},
						},
						Hostnames: []string{"hostname.com"},
						Meta: map[string]string{
							constants.MetaKeyKubeName: "name",
						},
						Status: api.ConfigEntryStatus{},
					},
				})[api.HTTPRoute],
			},
			want: []api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route 2",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-2",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
			},
		},
		{
			name: "same ref new cache has a greater modify index",
			oldCache: loadedReferenceMaps([]api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind:        api.HTTPRoute,
					Name:        "my route",
					ModifyIndex: 1,
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
			})[api.HTTPRoute],
			args: args{
				newCache: loadedReferenceMaps([]api.ConfigEntry{
					&api.HTTPRouteConfigEntry{
						Kind:        api.HTTPRoute,
						Name:        "my route",
						ModifyIndex: 10,
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Namespace:   "ns",
							},
						},
						Rules: []api.HTTPRouteRule{
							{
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"add it on": "the value",
											},
											Remove: []string{"time to go"},
											Set: map[string]string{
												"Magic":       "v2",
												"Another One": "dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{Path: "v1"},
								},
								Matches: []api.HTTPMatch{
									{
										Headers: []api.HTTPHeaderMatch{
											{
												Match: api.HTTPHeaderMatchExact,
												Name:  "my header match",
												Value: "the value",
											},
										},
										Method: api.HTTPMatchMethodGet,
										Path: api.HTTPPathMatch{
											Match: api.HTTPPathMatchPrefix,
											Value: "/v1",
										},
										Query: []api.HTTPQueryMatch{
											{
												Match: api.HTTPQueryMatchExact,
												Name:  "search",
												Value: "term",
											},
										},
									},
								},
								Services: []api.HTTPService{
									{
										Name:   "service one",
										Weight: 45,
										Filters: api.HTTPFilters{
											Headers: []api.HTTPHeaderFilter{
												{
													Add: map[string]string{
														"svc - add it on": "svc - the value",
													},
													Remove: []string{"svc - time to go"},
													Set: map[string]string{
														"svc - Magic":       "svc - v2",
														"svc - Another One": "svc - dj khaled",
													},
												},
											},
											URLRewrite: &api.URLRewrite{
												Path: "path",
											},
										},
										Namespace: "some ns",
									},
								},
							},
						},
						Hostnames: []string{"hostname.com"},
						Meta: map[string]string{
							constants.MetaKeyKubeName: "name",
						},
						Status: api.ConfigEntryStatus{},
					},
				})[api.HTTPRoute],
			},
			want: []api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind:        api.HTTPRoute,
					Name:        "my route",
					ModifyIndex: 10,
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Namespace:   "ns",
						},
					},
					Rules: []api.HTTPRouteRule{
						{
							Filters: api.HTTPFilters{
								Headers: []api.HTTPHeaderFilter{
									{
										Add: map[string]string{
											"add it on": "the value",
										},
										Remove: []string{"time to go"},
										Set: map[string]string{
											"Magic":       "v2",
											"Another One": "dj khaled",
										},
									},
								},
								URLRewrite: &api.URLRewrite{Path: "v1"},
							},
							Matches: []api.HTTPMatch{
								{
									Headers: []api.HTTPHeaderMatch{
										{
											Match: api.HTTPHeaderMatchExact,
											Name:  "my header match",
											Value: "the value",
										},
									},
									Method: api.HTTPMatchMethodGet,
									Path: api.HTTPPathMatch{
										Match: api.HTTPPathMatchPrefix,
										Value: "/v1",
									},
									Query: []api.HTTPQueryMatch{
										{
											Match: api.HTTPQueryMatchExact,
											Name:  "search",
											Value: "term",
										},
									},
								},
							},
							Services: []api.HTTPService{
								{
									Name:   "service one",
									Weight: 45,
									Filters: api.HTTPFilters{
										Headers: []api.HTTPHeaderFilter{
											{
												Add: map[string]string{
													"svc - add it on": "svc - the value",
												},
												Remove: []string{"svc - time to go"},
												Set: map[string]string{
													"svc - Magic":       "svc - v2",
													"svc - Another One": "svc - dj khaled",
												},
											},
										},
										URLRewrite: &api.URLRewrite{
											Path: "path",
										},
									},
									Namespace: "some ns",
								},
							},
						},
					},
					Hostnames: []string{"hostname.com"},
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
					Status: api.ConfigEntryStatus{},
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.oldCache.Diff(tt.args.newCache)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("resourceCache.diff mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCache_Subscribe(t *testing.T) {
	t.Parallel()
	type args struct {
		ctx        context.Context
		kind       string
		translator TranslatorFn
	}
	tests := []struct {
		name             string
		args             args
		subscribers      map[string][]*Subscription
		subscriberChange int
	}{
		{
			name: "new subscription added when there are no other subscribers of the same kind",
			args: args{
				ctx:  context.Background(),
				kind: api.HTTPRoute,
				translator: func(api.ConfigEntry) []types.NamespacedName {
					return []types.NamespacedName{}
				},
			},
			subscriberChange: 1,
		},
		{
			name: "new subscription added when there are existing subscribers of the same kind",
			args: args{
				ctx:  context.Background(),
				kind: api.HTTPRoute,
				translator: func(api.ConfigEntry) []types.NamespacedName {
					return []types.NamespacedName{}
				},
			},
			subscribers: map[string][]*Subscription{
				api.HTTPRoute: {
					{
						translator: func(api.ConfigEntry) []types.NamespacedName {
							return []types.NamespacedName{}
						},
						ctx: context.Background(),
						cancelCtx: func() {
						},
						events: make(chan event.GenericEvent),
					},
				},
			},
			subscriberChange: 1,
		},
		{
			name: "subscription for kind that does not exist does not change any subscriber counts",
			args: args{
				ctx:  context.Background(),
				kind: "UnknownKind",
				translator: func(api.ConfigEntry) []types.NamespacedName {
					return []types.NamespacedName{}
				},
			},
			subscriberChange: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(Config{
				ConsulClientConfig: &consul.Config{
					APIClientConfig: &api.Config{},
					HTTPPort:        0,
					GRPCPort:        0,
					APITimeout:      0,
				},
				ConsulServerConnMgr: consul.NewMockServerConnectionManager(t),
				NamespacesEnabled:   false,
				Logger:              logr.Logger{},
			})

			if len(tt.subscribers) > 0 {
				c.subscribers = tt.subscribers
			}

			kindSubscriberCounts := make(map[string]int)
			for kind, subscribers := range c.subscribers {
				kindSubscriberCounts[kind] = len(subscribers)
			}

			c.Subscribe(tt.args.ctx, tt.args.kind, tt.args.translator)

			for kind, subscribers := range c.subscribers {
				expectedSubscriberCount := kindSubscriberCounts[kind]
				if kind == tt.args.kind {
					expectedSubscriberCount += tt.subscriberChange
				}
				actualSubscriberCount := len(subscribers)

				if expectedSubscriberCount != actualSubscriberCount {
					t.Errorf("Expected there to be %d subscribers, there were %d", expectedSubscriberCount, actualSubscriberCount)
				}
			}
		})
	}
}

func TestCache_Write(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		responseFn  func(w http.ResponseWriter)
		expectedErr error
	}{
		{
			name: "write is successful",
			responseFn: func(w http.ResponseWriter) {
				w.WriteHeader(200)
				fmt.Fprintln(w, `{updated: true}`)
			},
			expectedErr: nil,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/config":
					tt.responseFn(w)
				case "/v1/catalog/services":
					fmt.Fprintln(w, `{}`)
				default:
					w.WriteHeader(500)
					fmt.Fprintln(w, "Mock Server not configured for this route: "+r.URL.Path)
				}
			}))
			defer consulServer.Close()

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)

			port, err := strconv.Atoi(serverURL.Port())
			require.NoError(t, err)

			c := New(Config{
				ConsulClientConfig: &consul.Config{
					APIClientConfig: &api.Config{},
					HTTPPort:        port,
					GRPCPort:        port,
					APITimeout:      0,
				},
				ConsulServerConnMgr: test.MockConnMgrForIPAndPort(t, serverURL.Hostname(), port, false),
				NamespacesEnabled:   false,
				Logger:              logrtest.NewTestLogger(t),
			})

			entry := &api.HTTPRouteConfigEntry{
				Kind: api.HTTPRoute,
				Name: "my route",
				Parents: []api.ResourceReference{
					{
						Kind:        api.APIGateway,
						Name:        "api-gw",
						SectionName: "listener-1",
						Namespace:   "ns",
					},
				},
				Rules: []api.HTTPRouteRule{
					{
						Filters: api.HTTPFilters{
							Headers: []api.HTTPHeaderFilter{
								{
									Add: map[string]string{
										"add it on": "the value",
									},
									Remove: []string{"time to go"},
									Set: map[string]string{
										"Magic":       "v2",
										"Another One": "dj khaled",
									},
								},
							},
							URLRewrite: &api.URLRewrite{Path: "v1"},
						},
						Matches: []api.HTTPMatch{
							{
								Headers: []api.HTTPHeaderMatch{
									{
										Match: api.HTTPHeaderMatchExact,
										Name:  "my header match",
										Value: "the value",
									},
								},
								Method: api.HTTPMatchMethodGet,
								Path: api.HTTPPathMatch{
									Match: api.HTTPPathMatchPrefix,
									Value: "/v1",
								},
								Query: []api.HTTPQueryMatch{
									{
										Match: api.HTTPQueryMatchExact,
										Name:  "search",
										Value: "term",
									},
								},
							},
						},
						Services: []api.HTTPService{
							{
								Name:   "service one",
								Weight: 45,
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"svc - add it on": "svc - the value",
											},
											Remove: []string{"svc - time to go"},
											Set: map[string]string{
												"svc - Magic":       "svc - v2",
												"svc - Another One": "svc - dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{
										Path: "path",
									},
								},
								Namespace: "some ns",
							},
						},
					},
				},
				Hostnames: []string{"hostname.com"},
				Meta: map[string]string{
					constants.MetaKeyKubeName: "name",
				},
				Status: api.ConfigEntryStatus{},
			}

			err = c.Write(context.Background(), entry)
			require.Equal(t, err, tt.expectedErr)
		})
	}
}

func TestCache_Get(t *testing.T) {
	t.Parallel()
	type args struct {
		ref api.ResourceReference
	}
	tests := []struct {
		name  string
		args  args
		want  api.ConfigEntry
		cache map[string]*common.ReferenceMap
	}{
		{
			name: "entry exists",
			args: args{
				ref: api.ResourceReference{
					Kind: api.APIGateway,
					Name: "api-gw",
				},
			},
			want: &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: "api-gw",
				Meta: map[string]string{
					constants.MetaKeyKubeName: "name",
				},
			},
			cache: loadedReferenceMaps([]api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
				},
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw-2",
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
				},
			}),
		},
		{
			name: "entry does not exist",
			args: args{
				ref: api.ResourceReference{
					Kind: api.APIGateway,
					Name: "api-gw-4",
				},
			},
			want: nil,
			cache: loadedReferenceMaps([]api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
				},
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw-2",
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
				},
			}),
		},
		{
			name: "kind key does not exist",
			args: args{
				ref: api.ResourceReference{
					Kind: api.APIGateway,
					Name: "api-gw-4",
				},
			},
			want: nil,
			cache: loadedReferenceMaps([]api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "route",
					Meta: map[string]string{
						constants.MetaKeyKubeName: "name",
					},
				},
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(Config{
				ConsulClientConfig: &consul.Config{
					APIClientConfig: &api.Config{},
				},
			})
			c.cache = tt.cache

			got := c.Get(tt.args.ref)

			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("Cache.Get mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Run(t *testing.T) {
	t.Parallel()
	// setup httproutes
	httpRouteOne, httpRouteTwo := setupHTTPRoutes()
	httpRoutes := []*api.HTTPRouteConfigEntry{httpRouteOne, httpRouteTwo}

	// setup gateway
	gw := setupGateway()
	gateways := []*api.APIGatewayConfigEntry{gw}

	// setup TCPRoutes
	tcpRoute := setupTCPRoute()
	tcpRoutes := []*api.TCPRouteConfigEntry{tcpRoute}

	// setup file-system certs
	fileSystemCert := setupFileSystemCertificate()
	certs := []*api.FileSystemCertificateConfigEntry{fileSystemCert}

	// setup jwt providers
	jwtProvider := setupJWTProvider()
	providers := []*api.JWTProviderConfigEntry{jwtProvider}

	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/config/http-route":
			val, err := json.Marshal(httpRoutes)
			if err != nil {
				w.WriteHeader(500)
				fmt.Fprintln(w, err)
				return
			}
			fmt.Fprintln(w, string(val))
		case "/v1/config/api-gateway":
			val, err := json.Marshal(gateways)
			if err != nil {
				w.WriteHeader(500)
				fmt.Fprintln(w, err)
				return
			}
			fmt.Fprintln(w, string(val))
		case "/v1/config/tcp-route":
			val, err := json.Marshal(tcpRoutes)
			if err != nil {
				w.WriteHeader(500)
				fmt.Fprintln(w, err)
				return
			}
			fmt.Fprintln(w, string(val))
		case "/v1/config/file-system-certificate":
			val, err := json.Marshal(certs)
			if err != nil {
				w.WriteHeader(500)
				fmt.Fprintln(w, err)
				return
			}
			fmt.Fprintln(w, string(val))
		case "/v1/config/jwt-provider":
			val, err := json.Marshal(providers)
			if err != nil {
				w.WriteHeader(500)
				fmt.Fprintln(w, err)
				return
			}
			fmt.Fprintln(w, string(val))
		case "/v1/catalog/services":
			fmt.Fprintln(w, `{}`)
		case "/v1/peerings":
			fmt.Fprintln(w, `[]`)
		default:
			w.WriteHeader(500)
			fmt.Fprintln(w, "Mock Server not configured for this route: "+r.URL.Path)
		}
	}))
	defer consulServer.Close()

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)

	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err)

	c := New(Config{
		ConsulClientConfig: &consul.Config{
			APIClientConfig: &api.Config{},
			HTTPPort:        port,
			GRPCPort:        port,
			APITimeout:      0,
		},
		ConsulServerConnMgr: test.MockConnMgrForIPAndPort(t, serverURL.Hostname(), port, false),
		NamespacesEnabled:   false,
		Logger:              logrtest.NewTestLogger(t),
	})
	prevCache := make(map[string]*common.ReferenceMap)
	for kind, cache := range c.cache {
		resCache := common.NewReferenceMap()
		for _, entry := range cache.Entries() {
			resCache.Set(common.EntryToReference(entry), entry)
		}
		prevCache[kind] = resCache
	}

	expectedCache := loadedReferenceMaps([]api.ConfigEntry{
		gw, tcpRoute, httpRouteOne, httpRouteTwo, fileSystemCert, jwtProvider,
	})

	ctx, cancelFn := context.WithCancel(context.Background())

	httpRouteOneNsn := types.NamespacedName{
		Name:      httpRouteOne.Name,
		Namespace: httpRouteOne.Namespace,
	}

	httpRouteTwoNsn := types.NamespacedName{
		Name:      httpRouteTwo.Name,
		Namespace: httpRouteTwo.Namespace,
	}

	httpRouteSubscriber := c.Subscribe(ctx, api.HTTPRoute, func(cfe api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			{Name: cfe.GetName(), Namespace: cfe.GetNamespace()},
		}
	})

	canceledSub := c.Subscribe(ctx, api.HTTPRoute, func(cfe api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			{Name: cfe.GetName(), Namespace: cfe.GetNamespace()},
		}
	})

	gwNsn := types.NamespacedName{
		Name:      gw.Name,
		Namespace: gw.Namespace,
	}

	gwSubscriber := c.Subscribe(ctx, api.APIGateway, func(cfe api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			{Name: cfe.GetName(), Namespace: cfe.GetNamespace()},
		}
	})

	tcpRouteNsn := types.NamespacedName{
		Name:      tcpRoute.Name,
		Namespace: tcpRoute.Namespace,
	}

	tcpRouteSubscriber := c.Subscribe(ctx, api.TCPRoute, func(cfe api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			{Name: cfe.GetName(), Namespace: cfe.GetNamespace()},
		}
	})

	certNsn := types.NamespacedName{
		Name:      fileSystemCert.Name,
		Namespace: fileSystemCert.Namespace,
	}

	certSubscriber := c.Subscribe(ctx, api.FileSystemCertificate, func(cfe api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			{Name: cfe.GetName(), Namespace: cfe.GetNamespace()},
		}
	})

	jwtProviderNsn := types.NamespacedName{
		Name:      jwtProvider.Name,
		Namespace: jwtProvider.Namespace,
	}

	jwtSubscriber := c.Subscribe(ctx, api.JWTProvider, func(cfe api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			{Name: cfe.GetName(), Namespace: cfe.GetNamespace()},
		}
	})
	// mark this subscription as ended
	canceledSub.Cancel()

	go c.Run(ctx)

	// Check subscribers
	httpRouteExpectedEvents := []event.GenericEvent{{Object: newConfigEntryObject(httpRouteOneNsn)}, {Object: newConfigEntryObject(httpRouteTwoNsn)}}
	gwExpectedEvent := event.GenericEvent{Object: newConfigEntryObject(gwNsn)}
	tcpExpectedEvent := event.GenericEvent{Object: newConfigEntryObject(tcpRouteNsn)}
	certExpectedEvent := event.GenericEvent{Object: newConfigEntryObject(certNsn)}
	jwtProviderExpectedEvent := event.GenericEvent{Object: newConfigEntryObject(jwtProviderNsn)}

	// 2 http routes + 1 gw + 1 tcp route + 1 cert + 1 jwtProvider = 6
	i := 6
	for {
		if i == 0 {
			break
		}
		select {
		case actualHTTPRouteEvent := <-httpRouteSubscriber.Events():
			require.Contains(t, httpRouteExpectedEvents, actualHTTPRouteEvent)
		case actualGWEvent := <-gwSubscriber.Events():
			require.Equal(t, gwExpectedEvent, actualGWEvent)
		case actualTCPRouteEvent := <-tcpRouteSubscriber.Events():
			require.Equal(t, tcpExpectedEvent, actualTCPRouteEvent)
		case actualCertExpectedEvent := <-certSubscriber.Events():
			require.Equal(t, certExpectedEvent, actualCertExpectedEvent)
		case actualJWTExpectedEvent := <-jwtSubscriber.Events():
			require.Equal(t, jwtProviderExpectedEvent, actualJWTExpectedEvent)
		}
		i -= 1
	}

	// the canceled Subscription should not receive any events
	require.Zero(t, len(canceledSub.Events()))
	c.WaitSynced(ctx)

	// cancel the context so the Run function exits
	cancelFn()

	sorter := func(x, y api.ConfigEntry) bool {
		return x.GetName() < y.GetName()
	}
	// Check cache
	// expect the cache to have changed
	for _, kind := range Kinds {
		if diff := cmp.Diff(prevCache[kind].Entries(), c.cache[kind].Entries(), cmpopts.SortSlices(sorter)); diff == "" {
			t.Error("Expect cache to have changed but it did not")
		}

		if diff := cmp.Diff(expectedCache[kind].Entries(), c.cache[kind].Entries(), cmpopts.SortSlices(sorter)); diff != "" {
			t.Errorf("Cache.cache mismatch (-want +got):\n%s", diff)
		}
	}
}

func setupHTTPRoutes() (*api.HTTPRouteConfigEntry, *api.HTTPRouteConfigEntry) {
	routeOne := &api.HTTPRouteConfigEntry{
		Kind: api.HTTPRoute,
		Name: "my route",
		Parents: []api.ResourceReference{
			{
				Kind:        api.APIGateway,
				Name:        "api-gw",
				SectionName: "listener-1",
				Namespace:   "ns",
			},
		},
		Rules: []api.HTTPRouteRule{
			{
				Filters: api.HTTPFilters{
					Headers: []api.HTTPHeaderFilter{
						{
							Add: map[string]string{
								"add it on": "the value",
							},
							Remove: []string{"time to go"},
							Set: map[string]string{
								"Magic":       "v2",
								"Another One": "dj khaled",
							},
						},
					},
					URLRewrite: &api.URLRewrite{Path: "v1"},
				},
				Matches: []api.HTTPMatch{
					{
						Headers: []api.HTTPHeaderMatch{
							{
								Match: api.HTTPHeaderMatchExact,
								Name:  "my header match",
								Value: "the value",
							},
						},
						Method: api.HTTPMatchMethodGet,
						Path: api.HTTPPathMatch{
							Match: api.HTTPPathMatchPrefix,
							Value: "/v1",
						},
						Query: []api.HTTPQueryMatch{
							{
								Match: api.HTTPQueryMatchExact,
								Name:  "search",
								Value: "term",
							},
						},
					},
				},
				Services: []api.HTTPService{
					{
						Name:   "service one",
						Weight: 45,
						Filters: api.HTTPFilters{
							Headers: []api.HTTPHeaderFilter{
								{
									Add: map[string]string{
										"svc - add it on": "svc - the value",
									},
									Remove: []string{"svc - time to go"},
									Set: map[string]string{
										"svc - Magic":       "svc - v2",
										"svc - Another One": "svc - dj khaled",
									},
								},
							},
							URLRewrite: &api.URLRewrite{
								Path: "path",
							},
						},
						Namespace: "some ns",
					},
				},
			},
		},
		Hostnames: []string{"hostname.com"},
		Meta: map[string]string{
			"metaKey":                 "metaVal",
			constants.MetaKeyKubeName: "name",
		},
		Status: api.ConfigEntryStatus{},
	}
	routeTwo := &api.HTTPRouteConfigEntry{
		Kind: api.HTTPRoute,
		Name: "my route 2",
		Parents: []api.ResourceReference{
			{
				Kind:        api.APIGateway,
				Name:        "api-gw",
				SectionName: "listener-2",
				Namespace:   "ns",
			},
		},
		Rules: []api.HTTPRouteRule{
			{
				Filters: api.HTTPFilters{
					Headers: []api.HTTPHeaderFilter{
						{
							Add: map[string]string{
								"add it on": "the value",
							},
							Remove: []string{"time to go"},
							Set: map[string]string{
								"Magic":       "v2",
								"Another One": "dj khaled",
							},
						},
					},
					URLRewrite: &api.URLRewrite{Path: "v1"},
				},
				Matches: []api.HTTPMatch{
					{
						Headers: []api.HTTPHeaderMatch{
							{
								Match: api.HTTPHeaderMatchExact,
								Name:  "my header match",
								Value: "the value",
							},
						},
						Method: api.HTTPMatchMethodGet,
						Path: api.HTTPPathMatch{
							Match: api.HTTPPathMatchPrefix,
							Value: "/v1",
						},
						Query: []api.HTTPQueryMatch{
							{
								Match: api.HTTPQueryMatchExact,
								Name:  "search",
								Value: "term",
							},
						},
					},
				},
				Services: []api.HTTPService{
					{
						Name:   "service one",
						Weight: 45,
						Filters: api.HTTPFilters{
							Headers: []api.HTTPHeaderFilter{
								{
									Add: map[string]string{
										"svc - add it on": "svc - the value",
									},
									Remove: []string{"svc - time to go"},
									Set: map[string]string{
										"svc - Magic":       "svc - v2",
										"svc - Another One": "svc - dj khaled",
									},
								},
							},
							URLRewrite: &api.URLRewrite{
								Path: "path",
							},
						},
						Namespace: "some ns",
					},
				},
			},
		},
		Hostnames: []string{"hostname.com"},
		Meta: map[string]string{
			"metakey":                 "meta val",
			constants.MetaKeyKubeName: "name",
		},
	}
	return routeOne, routeTwo
}

func setupGateway() *api.APIGatewayConfigEntry {
	return &api.APIGatewayConfigEntry{
		Kind: api.APIGateway,
		Name: "api-gw",
		Meta: map[string]string{
			"metakey":                 "meta val",
			constants.MetaKeyKubeName: "name",
		},
		Listeners: []api.APIGatewayListener{
			{
				Name:     "listener one",
				Hostname: "hostname.com",
				Port:     3350,
				Protocol: "https",
				TLS:      api.APIGatewayTLSConfiguration{},
			},
		},
	}
}

func setupTCPRoute() *api.TCPRouteConfigEntry {
	return &api.TCPRouteConfigEntry{
		Kind: api.TCPRoute,
		Name: "tcp route",
		Parents: []api.ResourceReference{
			{
				Kind:        api.APIGateway,
				Name:        "api-gw",
				SectionName: "listener two",
			},
		},
		Services: []api.TCPService{
			{
				Name: "tcp service",
			},
		},
		Meta: map[string]string{
			"metakey":                 "meta val",
			constants.MetaKeyKubeName: "name",
		},
		Status: api.ConfigEntryStatus{},
	}
}

func setupFileSystemCertificate() *api.FileSystemCertificateConfigEntry {
	return &api.FileSystemCertificateConfigEntry{
		Kind:        api.FileSystemCertificate,
		Name:        "file-system-cert",
		Certificate: "cert",
		PrivateKey:  "super secret",
		Meta: map[string]string{
			"metaKey":                 "meta val",
			constants.MetaKeyKubeName: "name",
		},
	}
}

func setupJWTProvider() *api.JWTProviderConfigEntry {
	return &api.JWTProviderConfigEntry{
		Kind: api.JWTProvider,
		Name: "okta",
	}
}

func TestCache_Delete(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		responseFn  func(w http.ResponseWriter)
		expectedErr error
	}{
		{
			name: "delete is successful",
			responseFn: func(w http.ResponseWriter) {
				w.WriteHeader(200)
				fmt.Fprintln(w, `{deleted: true}`)
			},
			expectedErr: nil,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ref := api.ResourceReference{
				Name: "my-route",
				Kind: api.HTTPRoute,
			}
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case fmt.Sprintf("/v1/config/%s/%s", ref.Kind, ref.Name):
					tt.responseFn(w)
				default:
					w.WriteHeader(500)
					fmt.Fprintln(w, "Mock Server not configured for this route: "+r.URL.Path)
				}
			}))
			defer consulServer.Close()

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)

			port, err := strconv.Atoi(serverURL.Port())
			require.NoError(t, err)

			c := New(Config{
				ConsulClientConfig: &consul.Config{
					APIClientConfig: &api.Config{},
					HTTPPort:        port,
					GRPCPort:        port,
					APITimeout:      0,
				},
				ConsulServerConnMgr: test.MockConnMgrForIPAndPort(t, serverURL.Hostname(), port, false),
				NamespacesEnabled:   false,
				Logger:              logrtest.NewTestLogger(t),
			})

			err = c.Delete(context.Background(), ref)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCache_RemoveRoleBinding(t *testing.T) {
	t.Parallel()
	successFn := func(w http.ResponseWriter) {
		w.WriteHeader(200)
		fmt.Fprintln(w, `{deleted: true}`)
	}

	notFoundFn := func(w http.ResponseWriter) {
		w.WriteHeader(404)
	}

	aclDisabledFn := func(w http.ResponseWriter) {
		w.WriteHeader(401)
		fmt.Fprintln(w, `ACL support disabled`)
	}

	errorFn := func(w http.ResponseWriter) {
		w.WriteHeader(500)
		fmt.Fprintln(w, `error`)
	}

	testCases := map[string]struct {
		bindingRule       *api.ACLBindingRule
		role              *api.ACLRole
		policy            *api.ACLPolicy
		bindingRuleRespFn func(w http.ResponseWriter)
		roleRespFn        func(w http.ResponseWriter)
		policyRespFn      func(w http.ResponseWriter)
		expectedErr       error
	}{
		"delete is successful": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: successFn,
			roleRespFn:        successFn,
			policyRespFn:      successFn,
			expectedErr:       nil,
		},
		"binding rule is not found": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: notFoundFn,
			roleRespFn:        successFn,
			policyRespFn:      successFn,
			expectedErr:       nil,
		},
		"role is not found": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: successFn,
			roleRespFn:        notFoundFn,
			policyRespFn:      successFn,
			expectedErr:       nil,
		},
		"policy is not found": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: successFn,
			roleRespFn:        successFn,
			policyRespFn:      notFoundFn,
			expectedErr:       nil,
		},
		"acl support is disabled": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: aclDisabledFn,
			expectedErr:       nil,
		},
		"failed to delete binding rule": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: errorFn,
			expectedErr:       ErrFailedToDeleteBindingRule,
		},
		"failed to delete role": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: successFn,
			roleRespFn:        errorFn,
			expectedErr:       ErrFailedToDeleteRole,
		},
		"failed to delete policy": {
			bindingRule: &api.ACLBindingRule{
				ID: "binding-rule-id",
			},
			role: &api.ACLRole{
				ID: "role-id",
			},
			policy: &api.ACLPolicy{
				ID: "policy-id",
			},
			bindingRuleRespFn: successFn,
			roleRespFn:        successFn,
			policyRespFn:      errorFn,
			expectedErr:       ErrFailedToDeletePolicy,
		},
	}
	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case fmt.Sprintf("/v1/acl/binding-rule/%s", tt.bindingRule.ID):
					tt.bindingRuleRespFn(w)
				case fmt.Sprintf("/v1/acl/role/%s", tt.role.ID):
					tt.roleRespFn(w)
				case fmt.Sprintf("/v1/acl/policy/%s", tt.policy.ID):
					tt.policyRespFn(w)
				default:
					w.WriteHeader(500)
					fmt.Fprintln(w, "Mock Server not configured for this route: "+r.URL.Path)
				}
			}))
			defer consulServer.Close()

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)

			port, err := strconv.Atoi(serverURL.Port())
			require.NoError(t, err)

			c := New(Config{
				ConsulClientConfig: &consul.Config{
					APIClientConfig: &api.Config{},
					HTTPPort:        port,
					GRPCPort:        port,
					APITimeout:      0,
				},
				ConsulServerConnMgr: test.MockConnMgrForIPAndPort(t, serverURL.Hostname(), port, false),
				NamespacesEnabled:   false,
				Logger:              logrtest.NewTestLogger(t),
			})

			authMethod := "k8s-auth-method"
			gatewayName := "my-api-gateway"
			namespace := "ns"
			// file the acl binding rule, acl policy, and acl role maps with the necessary data
			c.gatewayNameToACLBindingRule[gatewayName] = tt.bindingRule
			c.gatewayNameToACLRole[gatewayName] = tt.role
			c.gatewayNameToACLPolicy[gatewayName] = tt.policy

			err = c.RemoveRoleBinding(authMethod, gatewayName, namespace)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func loadedReferenceMaps(entries []api.ConfigEntry) map[string]*common.ReferenceMap {
	refs := make(map[string]*common.ReferenceMap)

	for _, entry := range entries {
		refMap, ok := refs[entry.GetKind()]
		if !ok {
			refMap = common.NewReferenceMap()
		}
		refMap.Set(common.EntryToReference(entry), entry)
		refs[entry.GetKind()] = refMap
	}
	return refs
}
