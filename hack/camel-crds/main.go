// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Script to parse a YAML CRD file and change all the
// snake_case keys to camelCase and rewrite the file in-situ
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iancoleman/strcase"
	"sigs.k8s.io/yaml"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Println("Usage: go run ./...")
		os.Exit(1)
	}

	if err := realMain(); err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func realMain() error {
	root := "../../control-plane/config/crd/"
	// explicitly ignore the `external` folder since we only want this to apply to CRDs that we have built-in this project.
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

			jsonBytes, err := yaml.YAMLToJSON(contentBytes)
			if err != nil {
				return err
			}
			fixedJsonBytes := convertKeys(jsonBytes)
			contentsCamel, err := yaml.JSONToYAML(fixedJsonBytes)
			return os.WriteFile(path, contentsCamel, os.ModePerm)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func convertKeys(j json.RawMessage) json.RawMessage {
	m := make(map[string]json.RawMessage)
	n := make([]json.RawMessage, 0)
	array := false
	if err := json.Unmarshal(j, &m); err != nil {
		// Not a JSON object
		errArray := json.Unmarshal(j, &n)
		if errArray != nil {
			return j
		} else {
			array = true
		}
	}
	if !array {
		for k, v := range m {
			if k == "annotations" {
				continue
			}
			var fixed string
			if !strings.Contains(k, "_") {
				fixed = k
			} else {
				fixed = strcase.ToLowerCamel(k)
			}
			delete(m, k)
			m[fixed] = convertKeys(v)
		}

		b, err := json.Marshal(m)
		if err != nil {
			fmt.Printf("something went wrong", err)
			return j
		}
		return b
	} else {
		for i, message := range n {
			fixed := convertKeys(message)
			n[i] = fixed
		}
		b, err := json.Marshal(n)
		if err != nil {
			fmt.Printf("something went wrong", err)
			return j
		}
		return b
	}
}

func printf(format string, args ...interface{}) {
	fmt.Println(fmt.Sprintf(format, args...))
}
