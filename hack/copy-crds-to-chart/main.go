// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Script to copy generated CRD yaml into chart directory and modify it to match
// the expected chart format (e.g. formatted YAML).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// HACK IT!
	requiresPeering = map[string]struct{}{
		"consul.hashicorp.com_peeringacceptors.yaml": {},
		"consul.hashicorp.com_peeringdialers.yaml":   {},
	}
)

func main() {
	if len(os.Args) != 1 {
		fmt.Println("Usage: go run ./...")
		os.Exit(1)
	}

	if err := realMain("../../charts/consul"); err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func realMain(helmPath string) error {
	root := "../../control-plane/config/crd/"
	dirs := []string{"bases"}

	for _, dir := range dirs {
		err := filepath.Walk(root+dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() || filepath.Ext(path) != ".yaml" || filepath.Base(path) == "kustomization.yaml" {
				return nil
			}

			printf("processing %s", filepath.Base(path))

			contentBytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			contents := string(contentBytes)

			// Strip leading newline.
			contents = strings.TrimPrefix(contents, "\n")

			if _, ok := requiresPeering[info.Name()]; ok {
				// Add {{- if and .Values.connectInject.enabled .Values.global.peering.enabled  }} {{- end }} wrapper.
				contents = fmt.Sprintf("{{- if and .Values.connectInject.enabled .Values.global.peering.enabled }}\n%s{{- end }}\n", contents)
			} else if dir == "external" {
				contents = fmt.Sprintf("{{- if and .Values.connectInject.enabled .Values.connectInject.apiGateway.manageExternalCRDs }}\n%s{{- end }}\n", contents)
			} else {
				// Add {{- if .Values.connectInject.enabled }} {{- end }} wrapper.
				contents = fmt.Sprintf("{{- if .Values.connectInject.enabled }}\n%s{{- end }}\n", contents)
			}

			// Add labels, this is hacky because we're relying on the line number
			// but it means we don't need to regex or yaml parse.
			splitOnNewlines := strings.Split(contents, "\n")
			labelLines := []string{
				`  labels:`,
				`    app: {{ template "consul.name" . }}`,
				`    chart: {{ template "consul.chart" . }}`,
				`    heritage: {{ .Release.Service }}`,
				`    release: {{ .Release.Name }}`,
				`    component: crd`,
			}
			var split int
			if dir == "bases" {
				split = 8
			} else {
				split = 9
			}
			withLabels := append(splitOnNewlines[0:split], append(labelLines, splitOnNewlines[split:]...)...)
			contents = strings.Join(withLabels, "\n")

			var crdName string
			if dir == "bases" {
				// Construct the destination filename.
				filenameSplit := strings.Split(info.Name(), "_")
				crdName = filenameSplit[1]
			} else if dir == "external" {
				filenameSplit := strings.Split(info.Name(), ".")
				crdName = filenameSplit[0] + "-external.yaml"
			}

			destinationPath := filepath.Join(helmPath, "templates", fmt.Sprintf("crd-%s", crdName))
			// Write it.
			printf("writing to %s", destinationPath)
			return os.WriteFile(destinationPath, []byte(contents), 0644)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func printf(format string, args ...interface{}) {
	fmt.Println(fmt.Sprintf(format, args...))
}

func formatCRDName(name string) string {
	name = strings.TrimSuffix(name, ".yaml")
	segments := strings.Split(name, "_")
	return fmt.Sprintf("%s.%s.yaml", segments[1], segments[0])
}
