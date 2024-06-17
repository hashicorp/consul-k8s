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

	"github.com/hashicorp/consul/api"
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
		requirePartitionCreated func(testClient *test.TestServerClient)
	}

	testCases := map[string]testCase{
		"simple": {
			requirePartitionCreated: func(testClient *test.TestServerClient) {
				consul, err := api.NewClient(testClient.Cfg.APIClientConfig)
				require.NoError(t, err)

				partition, _, err := consul.Partitions().Read(context.Background(), partitionName, nil)
				require.NoError(t, err)
				require.NotNil(t, partition)
				require.Equal(t, partitionName, partition.Name)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var serverCfg *testutil.TestServerConfig
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
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
		preCreatePartition         func(testClient *test.TestServerClient)
		requirePartitionNotCreated func(testClient *test.TestServerClient)
	}

	testCases := map[string]testCase{
		"simple": {
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
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var serverCfg *testutil.TestServerConfig
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
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

	var serverCfg *testutil.TestServerConfig
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
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
	}

	testClient.TestServer.Stop()
	startTime := time.Now()
	responseCode := cmd.Run(args)
	completeTime := time.Now()
	require.Equal(t, 1, responseCode)

	// While the timeout is 500ms, adding a buffer of 500ms ensures we account for
	// some buffer time required for the task to run and assignments to occur.
	require.WithinDuration(t, completeTime, startTime, timeout+500*time.Millisecond)
}
