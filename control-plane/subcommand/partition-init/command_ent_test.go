// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package partition_init

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/proto-public/pbresource"
	pbtenancy "github.com/hashicorp/consul/proto-public/pbtenancy/v2beta1"
	"github.com/hashicorp/consul/sdk/testutil"

	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  nil,
			expErr: "addresses must be set",
		},
		{
			flags:  []string{"-addresses", "foo"},
			expErr: "-partition must be set",
		},
		{
			flags: []string{
				"-addresses", "foo",
				"-partition", "bar",
				"-api-timeout", "0s",
			},
			expErr: "-api-timeout must be set to a value greater than 0",
		},
		{
			flags: []string{
				"-addresses", "foo",
				"-partition", "bar",
				"-log-level", "invalid",
			},
			expErr: "unknown log level: invalid",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(tt *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{UI: ui}
			exitCode := cmd.Run(c.flags)
			require.Equal(tt, 1, exitCode, ui.ErrorWriter.String())
			require.Contains(tt, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_PartitionCreate(t *testing.T) {
	partitionName := "test-partition"

	type testCase struct {
		v2tenancy               bool
		experiments             []string
		requirePartitionCreated func(testClient *test.TestServerClient)
	}

	testCases := map[string]testCase{
		"v2tenancy false": {
			v2tenancy:   false,
			experiments: []string{},
			requirePartitionCreated: func(testClient *test.TestServerClient) {
				consul, err := api.NewClient(testClient.Cfg.APIClientConfig)
				require.NoError(t, err)

				partition, _, err := consul.Partitions().Read(context.Background(), partitionName, nil)
				require.NoError(t, err)
				require.NotNil(t, partition)
				require.Equal(t, partitionName, partition.Name)
			},
		},
		"v2tenancy true": {
			v2tenancy:   true,
			experiments: []string{"resource-apis", "v2tenancy"},
			requirePartitionCreated: func(testClient *test.TestServerClient) {
				_, err := testClient.ResourceClient.Read(context.Background(), &pbresource.ReadRequest{
					Id: &pbresource.ID{
						Name: partitionName,
						Type: pbtenancy.PartitionType,
					},
				})
				require.NoError(t, err, "expected partition to be created")
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var serverCfg *testutil.TestServerConfig
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = tc.experiments
				serverCfg = c
			})

			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			cmd.init()
			args := []string{
				"-addresses=" + "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-partition", partitionName,
				"-timeout", "1m",
				"-enable-v2tenancy=" + strconv.FormatBool(tc.v2tenancy),
			}

			responseCode := cmd.Run(args)
			require.Equal(t, 0, responseCode)
			tc.requirePartitionCreated(testClient)
		})
	}
}

func TestRun_PartitionExists(t *testing.T) {
	partitionName := "test-partition"
	partitionDesc := "Created before test"

	type testCase struct {
		v2tenancy                  bool
		experiments                []string
		preCreatePartition         func(testClient *test.TestServerClient)
		requirePartitionNotCreated func(testClient *test.TestServerClient)
	}

	testCases := map[string]testCase{
		"v2tenancy false": {
			v2tenancy:   false,
			experiments: []string{},

			preCreatePartition: func(testClient *test.TestServerClient) {
				consul, err := api.NewClient(testClient.Cfg.APIClientConfig)
				require.NoError(t, err)

				_, _, err = consul.Partitions().Create(context.Background(), &api.Partition{
					Name:        partitionName,
					Description: partitionDesc,
				}, nil)
				require.NoError(t, err)
			},
			requirePartitionNotCreated: func(testClient *test.TestServerClient) {
				consul, err := api.NewClient(testClient.Cfg.APIClientConfig)
				require.NoError(t, err)

				partition, _, err := consul.Partitions().Read(context.Background(), partitionName, nil)
				require.NoError(t, err)
				require.NotNil(t, partition)
				require.Equal(t, partitionName, partition.Name)
				require.Equal(t, partitionDesc, partition.Description)
			},
		},
		"v2tenancy true": {
			v2tenancy:   true,
			experiments: []string{"resource-apis", "v2tenancy"},
			preCreatePartition: func(testClient *test.TestServerClient) {
				data, err := anypb.New(&pbtenancy.Partition{Description: partitionDesc})
				require.NoError(t, err)

				_, err = testClient.ResourceClient.Write(context.Background(), &pbresource.WriteRequest{
					Resource: &pbresource.Resource{
						Id: &pbresource.ID{
							Name: partitionName,
							Type: pbtenancy.PartitionType,
						},
						Data: data,
					},
				})
				require.NoError(t, err)
			},
			requirePartitionNotCreated: func(testClient *test.TestServerClient) {
				rsp, err := testClient.ResourceClient.Read(context.Background(), &pbresource.ReadRequest{
					Id: &pbresource.ID{
						Name: partitionName,
						Type: pbtenancy.PartitionType,
					},
				})
				require.NoError(t, err)

				partition := &pbtenancy.Partition{}
				err = anypb.UnmarshalTo(rsp.Resource.Data, partition, proto.UnmarshalOptions{})
				require.NoError(t, err)
				require.Equal(t, partitionDesc, partition.Description)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var serverCfg *testutil.TestServerConfig
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = tc.experiments
				serverCfg = c
			})

			// Create the Admin Partition before the test runs.
			tc.preCreatePartition(testClient)

			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			cmd.init()
			args := []string{
				"-addresses=" + "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-partition", partitionName,
				"-enable-v2tenancy=" + strconv.FormatBool(tc.v2tenancy),
			}

			responseCode := cmd.Run(args)
			require.Equal(t, 0, responseCode)

			// Verify that the Admin Partition was not overwritten.
			tc.requirePartitionNotCreated(testClient)
		})
	}
}

func TestRun_ExitsAfterTimeout(t *testing.T) {
	partitionName := "test-partition"

	type testCase struct {
		v2tenancy   bool
		experiments []string
	}

	testCases := map[string]testCase{
		"v2tenancy false": {
			v2tenancy:   false,
			experiments: []string{},
		},
		"v2tenancy true": {
			v2tenancy:   true,
			experiments: []string{"resource-apis", "v2tenancy"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var serverCfg *testutil.TestServerConfig
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = tc.experiments
				serverCfg = c
			})

			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			cmd.init()

			timeout := 500 * time.Millisecond
			args := []string{
				"-addresses=" + "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-timeout", timeout.String(),
				"-partition", partitionName,
				"-enable-v2tenancy=" + strconv.FormatBool(tc.v2tenancy),
			}

			testClient.TestServer.Stop()
			startTime := time.Now()
			responseCode := cmd.Run(args)
			completeTime := time.Now()
			require.Equal(t, 1, responseCode)

			// While the timeout is 500ms, adding a buffer of 500ms ensures we account for
			// some buffer time required for the task to run and assignments to occur.
			require.WithinDuration(t, completeTime, startTime, timeout+500*time.Millisecond)
		})
	}
}
