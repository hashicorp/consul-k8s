// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package common

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestOpenShiftUID(t *testing.T) {
	cases := []struct {
		Name      string
		Namespace func() *corev1.Namespace
		Expected  int64
		Err       string
	}{
		{
			Name: "Valid uid annotation with slash",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationOpenShiftUIDRange: "1000700000/100000",
						},
					},
				}
				return ns
			},
			Expected: 1000700000,
			Err:      "",
		},
		{
			Name: "Valid uid annotation with dash",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationOpenShiftUIDRange: "1234-1000",
						},
					},
				}
				return ns
			},
			Expected: 1234,
			Err:      "",
		},
		{
			Name: "Invalid uid annotation missing slash or dash",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							// annotation should have a slash '/' or dash '-'
							constants.AnnotationOpenShiftUIDRange: "5678",
						},
					},
				}
				return ns
			},
			Expected: 0,
			Err: fmt.Sprintf(
				"annotation %s contains an invalid format for value %s",
				constants.AnnotationOpenShiftUIDRange,
				"5678",
			),
		},
		{
			Name: "Missing uid annotation",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
					},
				}
				return ns
			},
			Expected: 0,
			Err:      fmt.Sprintf("unable to find annotation %s", constants.AnnotationOpenShiftUIDRange),
		},
		{
			Name: "Empty",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationOpenShiftUIDRange: "",
						},
					},
				}
				return ns
			},
			Expected: 0,
			Err:      "found annotation openshift.io/sa.scc.uid-range but it was empty",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			actual, err := GetOpenShiftUID(tt.Namespace(), SelectFirstInRange)
			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.Expected, actual)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestOpenShiftGroup(t *testing.T) {
	cases := []struct {
		Name      string
		Namespace func() *corev1.Namespace
		Expected  int64
		Err       string
	}{
		{
			Name: "Valid group annotation with slash",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationOpenShiftGroups: "123456789/1000",
						},
					},
				}
				return ns
			},
			Expected: 123456789,
			Err:      "",
		},
		{
			Name: "Valid group annotation with comma",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationOpenShiftGroups: "1234,1000",
						},
					},
				}
				return ns
			},
			Expected: 1234,
			Err:      "",
		},
		{
			Name: "Invalid group annotation missing slash or comma",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							// annotation should have a slash '/' or comma ','
							constants.AnnotationOpenShiftGroups: "5678",
						},
					},
				}
				return ns
			},
			Expected: 0,
			Err: fmt.Sprintf(
				"annotation %s contains an invalid format for value %s",
				constants.AnnotationOpenShiftGroups,
				"5678",
			),
		},
		{
			Name: "Missing group annotation, fall back to UID annotation",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
						Annotations: map[string]string{
							// annotation should have a slash '/' or comma ','
							constants.AnnotationOpenShiftUIDRange: "9012/1000",
						},
					},
				}
				return ns
			},
			Expected: 9012,
			Err:      "",
		},
		{
			Name: "Missing both group and fallback uid annotation",
			Namespace: func() *corev1.Namespace {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: "default",
					},
				}
				return ns
			},
			Expected: 0,
			Err: fmt.Sprintf(
				"unable to find annotation %s or %s",
				constants.AnnotationOpenShiftGroups,
				constants.AnnotationOpenShiftUIDRange,
			),
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			actual, err := GetOpenShiftGroup(tt.Namespace(), SelectFirstInRange)
			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.Expected, actual)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}
