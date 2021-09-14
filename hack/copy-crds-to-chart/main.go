// Script to copy generated CRD yaml into chart directory and modify it to match
// the expected chart format (e.g. formatted YAML).
package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
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
	return filepath.Walk("../../control-plane/config/crd/bases", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}

		printf("processing %s", filepath.Base(path))

		contentBytes, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		contents := string(contentBytes)

		// Strip leading newline.
		contents = strings.TrimPrefix(contents, "\n")

		// Add {{- if .Values.controller.enabled }} {{- end }} wrapper.
		contents = fmt.Sprintf("{{- if .Values.controller.enabled }}\n%s{{- end }}\n", contents)

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
		withLabels := append(splitOnNewlines[0:9], append(labelLines, splitOnNewlines[9:]...)...)
		contents = strings.Join(withLabels, "\n")

		// Construct the destination filename.
		filenameSplit := strings.Split(info.Name(), "_")
		crdName := filenameSplit[1]
		destinationPath := filepath.Join(helmPath, "templates", fmt.Sprintf("crd-%s", crdName))

		// Write it.
		printf("writing to %s", destinationPath)
		return ioutil.WriteFile(destinationPath, []byte(contents), 0644)
	})
}

func printf(format string, args ...interface{}) {
	fmt.Println(fmt.Sprintf(format, args...))
}
