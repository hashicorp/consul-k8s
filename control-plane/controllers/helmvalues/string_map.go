// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helmvalues

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// StringMap is a map[string]string that unmarshals from either a JSON object or
// a JSON string containing a YAML map.
//
// Annotation values in the Helm chart are documented as multi-line YAML strings,
// and older chart versions wrote them into the helm-values ConfigMap verbatim as
// JSON strings. Accepting both forms keeps the controller working regardless of
// which chart version rendered the ConfigMap. Template expressions inside a
// string value are not evaluated; current chart versions render them before
// writing the ConfigMap.
type StringMap map[string]string

func (m *StringMap) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*m = nil
		return nil
	}

	var raw map[string]interface{}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if strings.TrimSpace(s) == "" {
			*m = nil
			return nil
		}
		if err := yaml.Unmarshal([]byte(s), &raw); err != nil {
			return fmt.Errorf("expected a YAML map of strings: %w", err)
		}
	} else {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&raw); err != nil {
			return err
		}
	}

	out := make(StringMap, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			out[k] = val
		case nil:
			out[k] = ""
		case map[string]interface{}, []interface{}:
			return fmt.Errorf("value for key %q must be a scalar, got %T", k, v)
		default:
			// Numbers and booleans, e.g. an unquoted port number.
			out[k] = fmt.Sprint(val)
		}
	}
	*m = out
	return nil
}
