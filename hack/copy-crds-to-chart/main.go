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

	// includeV1Suffix is used to add a ...-v1.yaml suffix for types that exist in
	// v1 and v2 APIs with the same name and would otherwise result in last man wins
	includeV1Suffix = map[string]struct{}{
		"consul.hashicorp.com_exportedservices.yaml":    {},
		"consul.hashicorp.com_gatewayclassconfigs.yaml": {},
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
	dirs := []string{"bases", "external"}

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
				// TCP Route is special, as it isn't installed onto GKE Autopilot, so it needs to have the option for `manageNonStandardCRDs`.
				if info.Name() == "tcproutes.gateway.networking.k8s.io.yaml" {
					contents = fmt.Sprintf("{{- if and .Values.connectInject.enabled (or .Values.connectInject.apiGateway.manageExternalCRDs .Values.connectInject.apiGateway.manageNonStandardCRDs ) }}\n%s{{- end }}\n", contents)
				} else {
					contents = fmt.Sprintf("{{- if and .Values.connectInject.enabled .Values.connectInject.apiGateway.manageExternalCRDs }}\n%s{{- end }}\n", contents)
				}
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
				split = 6
			} else {
				split = 9
			}
			withLabels := append(splitOnNewlines[0:split], append(labelLines, splitOnNewlines[split:]...)...)
			contents = strings.Join(withLabels, "\n")

			suffix := ""
			if _, ok := includeV1Suffix[info.Name()]; ok {
				suffix = "-v1"
			}

			var crdName string
			if dir == "bases" {
				// Construct the destination filename.
				filenameSplit := strings.Split(info.Name(), "_")
				filenameSplit = strings.Split(filenameSplit[1], ".")
				crdName = filenameSplit[0] + suffix + ".yaml"
			} else if dir == "external" {
				filenameSplit := strings.Split(info.Name(), ".")
				crdName = filenameSplit[0] + suffix + "-external.yaml"
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
