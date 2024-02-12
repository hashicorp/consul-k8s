// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package envoy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetLoggerLevelSucceeds(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		levelsToSet              [][]string
		expectedIndividualLevels []logLevel
	}{
		"single log level change trace": {
			levelsToSet: [][]string{
				{"admin", "trace"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "trace"},
			},
		},
		"single log level change debug": {
			levelsToSet: [][]string{
				{"admin", "debug"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "debug"},
			},
		},
		"single log level change info": {
			levelsToSet: [][]string{
				{"admin", "info"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "info"},
			},
		},
		"single log level change warning": {
			levelsToSet: [][]string{
				{"admin", "warning"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "warning"},
			},
		},
		"single log level change error": {
			levelsToSet: [][]string{
				{"admin", "error"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "error"},
			},
		},
		"single log level change critical": {
			levelsToSet: [][]string{
				{"admin", "critical"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "critical"},
			},
		},
		"single log level change off": {
			levelsToSet: [][]string{
				{"admin", "off"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "off"},
			},
		},
		"multiple log level change": {
			levelsToSet: [][]string{
				{"admin", "info"},
				{"grpc", "debug"},
			},
			expectedIndividualLevels: []logLevel{
				{name: "admin", level: "info"},
				{name: "grpc", level: "debug"},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			loggerParams := NewLoggerParams()
			for _, loggerLevel := range tc.levelsToSet {
				logger, level := loggerLevel[0], loggerLevel[1]
				err := loggerParams.SetLoggerLevel(logger, level)
				require.NoError(t, err)
			}
			require.Equal(t, loggerParams.individualLevels, tc.expectedIndividualLevels)
		})
	}
}

func TestSetLoggerLevelFails(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		loggerName  string
		loggerLevel string
	}{
		"invalid logger name": {
			loggerName:  "this is not the logger you're looking for",
			loggerLevel: "info",
		},
		"invalid logger level": {
			loggerName:  "grpc",
			loggerLevel: "this is also incorrect",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			loggerParams := NewLoggerParams()
			err := loggerParams.SetLoggerLevel(tc.loggerName, tc.loggerLevel)
			require.Error(t, err)
		})
	}
}

func TestSetGlobalLoggerLevel(t *testing.T) {
	t.Parallel()
	for level := range envoyLevels {
		loggerParams := NewLoggerParams()
		err := loggerParams.SetGlobalLoggerLevel(level)
		require.NoError(t, err)
	}
}

func TestSetGlobalLoggerLevelFails(t *testing.T) {
	t.Parallel()
	loggerParams := NewLoggerParams()
	err := loggerParams.SetGlobalLoggerLevel("not a valid level")
	require.Error(t, err)
}

func TestString(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		subject        *LoggerParams
		expectedOutput string
	}{
		"when global level is set": {
			subject:        &LoggerParams{globalLevel: "warn"},
			expectedOutput: "?level=warn",
		},
		"when one specific log level is set": {
			subject: &LoggerParams{
				individualLevels: []logLevel{
					{name: "grpc", level: "warn"},
				},
			},
			expectedOutput: "?grpc=warn",
		},
		"when multiple specific log levels are set": {
			subject: &LoggerParams{
				individualLevels: []logLevel{
					{name: "grpc", level: "warn"},
					{name: "http", level: "info"},
				},
			},
			expectedOutput: "?paths=grpc:warn,http:info",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			actual := tc.subject.String()
			require.Equal(t, actual, tc.expectedOutput)
		})
	}
}
