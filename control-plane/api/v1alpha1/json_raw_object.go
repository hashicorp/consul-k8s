// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

// JSONRawObject stores arbitrary JSON bytes while exposing an object schema in CRDs.
// Validation that the payload is a JSON object happens in the callers' validate methods.
// +kubebuilder:validation:XPreserveUnknownFields
type JSONRawObject []byte

func (j JSONRawObject) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return j, nil
}

func (j *JSONRawObject) UnmarshalJSON(data []byte) error {
	if j == nil {
		return nil
	}
	*j = append((*j)[:0], data...)
	return nil
}

func (JSONRawObject) OpenAPISchemaType() []string {
	return []string{"object"}
}

func (JSONRawObject) OpenAPISchemaFormat() string {
	return ""
}
