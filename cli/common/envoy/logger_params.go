// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package envoy

import (
	"fmt"
	"strings"
)

type logLevel struct {
	name  string
	level string
}

type LoggerParams struct {
	globalLevel      string
	individualLevels []logLevel
}

func NewLoggerParams() *LoggerParams {
	return &LoggerParams{
		individualLevels: make([]logLevel, 0),
	}
}

func (l *LoggerParams) SetLoggerLevel(name, level string) error {
	err := validateLoggerName(name)
	if err != nil {
		return err
	}
	err = validateLogLevel(level)
	if err != nil {
		return err
	}

	l.individualLevels = append(l.individualLevels, logLevel{name: name, level: level})
	return nil
}

func (l *LoggerParams) SetGlobalLoggerLevel(level string) error {
	err := validateLogLevel(level)
	if err != nil {
		return err
	}
	l.globalLevel = level
	return nil
}

func validateLogLevel(level string) error {
	if _, ok := envoyLevels[level]; !ok {
		logLevels := []string{}
		for levelName := range envoyLevels {
			logLevels = append(logLevels, levelName)
		}
		return fmt.Errorf("Unknown log level %s, available log levels are %q", level, strings.Join(logLevels, ", "))
	}
	return nil
}

func validateLoggerName(name string) error {
	if _, ok := EnvoyLoggers[name]; !ok {
		loggers := []string{}
		for loggerName := range envoyLevels {
			loggers = append(loggers, loggerName)
		}
		return fmt.Errorf("Unknown logger %s, available loggers are %q", name, strings.Join(loggers, ", "))

	}
	return nil
}

func (l *LoggerParams) String() string {
	switch {
	// Global log level change is set
	case l.globalLevel != "":
		return fmt.Sprintf("?level=%s", l.globalLevel)

		// only one specific logger is changed
	case len(l.individualLevels) == 1:
		params := fmt.Sprintf("?%s=%s", l.individualLevels[0].name, l.individualLevels[0].level)
		return params

		// multiple specific loggers are changed
	case len(l.individualLevels) > 1:
		logParams := make([]string, 0, len(l.individualLevels))
		for _, logger := range l.individualLevels {
			logParams = append(logParams, fmt.Sprintf("%s:%s", logger.name, logger.level))
		}

		params := fmt.Sprintf("?paths=%s", strings.Join(logParams, ","))
		return params
	default:

		// default path, this is hit if there are no params
		return ""
	}
}

// trace debug info warning error critical off.
var envoyLevels = map[string]struct{}{
	"trace":    {},
	"debug":    {},
	"info":     {},
	"warning":  {},
	"error":    {},
	"critical": {},
	"off":      {},
}

var EnvoyLoggers = map[string]struct{}{
	"admin":                     {},
	"alternate_protocols_cache": {},
	"aws":                       {},
	"assert":                    {},
	"backtrace":                 {},
	"cache_filter":              {},
	"client":                    {},
	"config":                    {},
	"connection":                {},
	"conn_handler":              {},
	"decompression":             {},
	"dns":                       {},
	"dubbo":                     {},
	"envoy_bug":                 {},
	"ext_authz":                 {},
	"ext_proc":                  {},
	"rocketmq":                  {},
	"file":                      {},
	"filter":                    {},
	"forward_proxy":             {},
	"grpc":                      {},
	"happy_eyeballs":            {},
	"hc":                        {},
	"health_checker":            {},
	"http":                      {},
	"http2":                     {},
	"hystrix":                   {},
	"init":                      {},
	"io":                        {},
	"jwt":                       {},
	"kafka":                     {},
	"key_value_store":           {},
	"lua":                       {},
	"main":                      {},
	"matcher":                   {},
	"misc":                      {},
	"mongo":                     {},
	"multi_connection":          {},
	"oauth2":                    {},
	"quic":                      {},
	"quic_stream":               {},
	"pool":                      {},
	"rbac":                      {},
	"rds":                       {},
	"redis":                     {},
	"router":                    {},
	"runtime":                   {},
	"stats":                     {},
	"secret":                    {},
	"tap":                       {},
	"testing":                   {},
	"thrift":                    {},
	"tracing":                   {},
	"upstream":                  {},
	"udp":                       {},
	"wasm":                      {},
	"websocket":                 {},
}
