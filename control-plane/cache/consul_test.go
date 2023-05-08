package cache

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul/api"
)

func Test_resourceCache_diff(t *testing.T) {
	type args struct {
		newCache resourceCache
	}
	tests := []struct {
		name     string
		oldCache resourceCache
		args     args
		want     []api.ConfigEntry
	}{
		{
			name: "no difference",
			oldCache: resourceCache{
				api.ResourceReference{
					Kind: api.HTTPRoute,
					Name: "my route",
				}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
			},
			args: args{
				newCache: resourceCache{
					api.ResourceReference{
						Kind: api.HTTPRoute,
						Name: "my route",
					}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Partition:   "part-1",
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
						Meta:      map[string]string{},
						Status:    api.ConfigEntryStatus{},
					}),
				},
			},
			want: []api.ConfigEntry{},
		},
		{
			name: "resource exists in old cache but not new one",
			oldCache: resourceCache{
				api.ResourceReference{
					Kind: api.HTTPRoute,
					Name: "my route",
				}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
				api.ResourceReference{
					Kind: api.HTTPRoute,
					Name: "my route 2",
				}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route 2",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-2",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
			},
			args: args{
				newCache: resourceCache{
					api.ResourceReference{
						Kind: api.HTTPRoute,
						Name: "my route",
					}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Partition:   "part-1",
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
						Meta:      map[string]string{},
						Status:    api.ConfigEntryStatus{},
					}),
				},
			},
			want: []api.ConfigEntry{
				api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route 2",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-2",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
			},
		},
		{
			name: "resource exists in new cache but not old one",
			oldCache: resourceCache{
				api.ResourceReference{
					Kind: api.HTTPRoute,
					Name: "my route",
				}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
			},
			args: args{
				newCache: resourceCache{
					api.ResourceReference{
						Kind: api.HTTPRoute,
						Name: "my route",
					}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Partition:   "part-1",
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
						Meta:      map[string]string{},
						Status:    api.ConfigEntryStatus{},
					}),

					api.ResourceReference{
						Kind: api.HTTPRoute,
						Name: "my route 2",
					}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
						Kind: api.HTTPRoute,
						Name: "my route 2",
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-2",
								Partition:   "part-1",
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
						Meta:      map[string]string{},
						Status:    api.ConfigEntryStatus{},
					}),
				},
			},
			want: []api.ConfigEntry{
				api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "my route 2",
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-2",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
			},
		},
		{
			name: "same ref new cache has a greater modify index",
			oldCache: resourceCache{
				api.ResourceReference{
					Kind: api.HTTPRoute,
					Name: "my route",
				}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind:        api.HTTPRoute,
					Name:        "my route",
					ModifyIndex: 1,
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
			},
			args: args{
				newCache: resourceCache{
					api.ResourceReference{
						Kind: api.HTTPRoute,
						Name: "my route",
					}: api.ConfigEntry(&api.HTTPRouteConfigEntry{
						Kind:        api.HTTPRoute,
						Name:        "my route",
						ModifyIndex: 10,
						Parents: []api.ResourceReference{
							{
								Kind:        api.APIGateway,
								Name:        "api-gw",
								SectionName: "listener-1",
								Partition:   "part-1",
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
						Meta:      map[string]string{},
						Status:    api.ConfigEntryStatus{},
					}),
				},
			},
			want: []api.ConfigEntry{
				api.ConfigEntry(&api.HTTPRouteConfigEntry{
					Kind:        api.HTTPRoute,
					Name:        "my route",
					ModifyIndex: 10,
					Parents: []api.ResourceReference{
						{
							Kind:        api.APIGateway,
							Name:        "api-gw",
							SectionName: "listener-1",
							Partition:   "part-1",
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
					Meta:      map[string]string{},
					Status:    api.ConfigEntryStatus{},
				}),
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.oldCache.diff(tt.args.newCache)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("resourceCache.diff mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCache_Subscribe(t *testing.T) {
	type args struct {
		ctx        context.Context
		kind       string
		translator Translator
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
				ConsulClientConfig:  &consul.Config{},
				ConsulServerConnMgr: consul.NewMockServerConnectionManager(t),
				NamespacesEnabled:   false,
				Kinds:               []string{api.APIGateway, api.TCPRoute, api.HTTPRoute},
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

func TestCache_notifySubscribers(t *testing.T) {
	ctx := context.Background()
	kind := api.HTTPRoute
	entries := []api.ConfigEntry{&api.HTTPRouteConfigEntry{}}

	c := New(Config{
		ConsulClientConfig:  &consul.Config{},
		ConsulServerConnMgr: consul.NewMockServerConnectionManager(t),
		NamespacesEnabled:   false,
		Kinds:               []string{api.APIGateway, api.HTTPRoute, api.TCPRoute, api.InlineCertificate},
		Logger:              logr.Logger{},
	})

	nsn := types.NamespacedName{
		Namespace: "ns",
		Name:      "my route",
	}

	expectedEvent := event.GenericEvent{Object: newConfigEntryObject(nsn)}

	sub1 := c.Subscribe(ctx, kind, func(_ api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			nsn,
		}
	})

	canceledSub := c.Subscribe(ctx, kind, func(_ api.ConfigEntry) []types.NamespacedName {
		return []types.NamespacedName{
			{
				Namespace: "ns",
				Name:      "my route",
			},
		}
	})

	// mark this subscription as ended
	canceledSub.cancelCtx()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func(w *sync.WaitGroup) {
		defer w.Done()
		c.notifySubscribers(ctx, kind, entries)
	}(wg)

	actualEvent := <-sub1.Events()

	// ensure everything is done
	wg.Wait()

	if diff := cmp.Diff(actualEvent, expectedEvent); diff != "" {
		t.Errorf("Cache.notifySubscribers mismatch (-want +got):\n%s", diff)
	}

	// we started with two subscribers
	if len(c.subscribers[kind]) != 1 || slices.Contains(c.subscribers[kind], canceledSub) {
		t.Error("Expected the canceled subscription to be removed from the active subscribers but it was not")
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
		{
			name: "stale entry",
			responseFn: func(w http.ResponseWriter) {
				w.WriteHeader(200)
				fmt.Fprintln(w, `{updated: false}`)
			},
			expectedErr: ErrStaleEntry,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/config":
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
				ConsulServerConnMgr: test.MockConnMgrForIPAndPort(serverURL.Hostname(), port),
				NamespacesEnabled:   false,
				Partition:           "",
				Kinds:               []string{},
				Logger:              logr.Logger{},
			})

			entry := &api.HTTPRouteConfigEntry{
				Kind: api.HTTPRoute,
				Name: "my route",
				Parents: []api.ResourceReference{
					{
						Kind:        api.APIGateway,
						Name:        "api-gw",
						SectionName: "listener-1",
						Partition:   "part-1",
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
				Meta:      map[string]string{},
				Status:    api.ConfigEntryStatus{},
			}

			err = c.Write(entry)
			require.Equal(t, err, tt.expectedErr)
		})
	}
}
