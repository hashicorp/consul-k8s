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

			if info.IsDir() || filepath.Ext(path) != ".yaml" || filepath.Base(path) == "kustomization.yaml" || strings.HasPrefix(info.Name(), ".") {
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
			if strings.TrimSpace(contents) == "" {
				printf("skipping empty %s", filepath.Base(path))
				return nil
			}

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

			contents, err = addCRDLabels(contents)
			if err != nil {
				return err
			}

			suffix := ""
			if _, ok := includeV1Suffix[info.Name()]; ok {
				suffix = "-v1"
			}

			crdName, err := destinationCRDName(info.Name(), dir, suffix)
			if err != nil {
				return err
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

func addCRDLabels(contents string) (string, error) {
	lines := strings.Split(contents, "\n")
	metadataIndex := -1
	insertIndex := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "metadata:" {
			metadataIndex = i
			insertIndex = i + 1
			break
		}
	}
	if metadataIndex == -1 {
		return "", fmt.Errorf("metadata block not found")
	}

	for i := metadataIndex + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(lines[i], "  ") {
			break
		}
		insertIndex = i + 1
		if strings.HasPrefix(trimmed, "name:") {
			break
		}
	}

	labelLines := []string{
		`  labels:`,
		`    app: {{ template "consul.name" . }}`,
		`    chart: {{ template "consul.chart" . }}`,
		`    heritage: {{ .Release.Service }}`,
		`    release: {{ .Release.Name }}`,
		`    component: crd`,
	}

	withLabels := append(lines[:insertIndex], append(labelLines, lines[insertIndex:]...)...)
	return strings.Join(withLabels, "\n"), nil
}

func destinationCRDName(filename, dir, suffix string) (string, error) {
	switch dir {
	case "bases":
		parts := strings.SplitN(filename, "_", 2)
		if len(parts) != 2 || parts[1] == "" {
			return "", fmt.Errorf("invalid base CRD filename: %s", filename)
		}
		nameParts := strings.SplitN(parts[1], ".", 2)
		if len(nameParts) == 0 || nameParts[0] == "" {
			return "", fmt.Errorf("invalid base CRD filename: %s", filename)
		}
		return nameParts[0] + suffix + ".yaml", nil
	case "external":
		name := strings.TrimSuffix(filename, ".yaml")
		if name == "" {
			return "", fmt.Errorf("invalid external CRD filename: %s", filename)
		}
		return strings.SplitN(name, ".", 2)[0] + suffix + "-external.yaml", nil
	default:
		return "", fmt.Errorf("unsupported CRD directory: %s", dir)
	}
}

func formatCRDName(name string) string {
	name = strings.TrimSuffix(name, ".yaml")
	segments := strings.Split(name, "_")
	return fmt.Sprintf("%s.%s.yaml", segments[1], segments[0])
}
