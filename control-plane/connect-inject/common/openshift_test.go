// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package common

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// TODO: Melisa add a test for application taking last UID/GroupID in the range (also second to last)

//func TestGetDatplaneUID(t *testing.T) {
//	cases := []struct {
//		Name      string
//		Namespace func() *corev1.Namespace
//		Pod       func() *corev1.Pod
//		Expected  int64
//		Err       string
//	}{
//		{
//			Name: "Valid uid annotation with slash",
//			Namespace: func() *corev1.Namespace {
//				ns := &corev1.Namespace{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:      "default",
//						Namespace: "default",
//						Annotations: map[string]string{
//							constants.AnnotationOpenShiftUIDRange: "1000700000/100000",
//						},
//					},
//				}
//				pod := &corev1.Pod{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:      "pod",
//					}
//				}
//				return ns
//			},
//			Expected: 1000700000,
//			Err:      "",
//		},
//		{
//			Name: "Valid uid annotation with dash",
//			Namespace: func() *corev1.Namespace {
//				ns := &corev1.Namespace{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:      "default",
//						Namespace: "default",
//						Annotations: map[string]string{
//							constants.AnnotationOpenShiftUIDRange: "1234-1000",
//						},
//					},
//				}
//				return ns
//			},
//			Expected: 1234,
//			Err:      "",
//		},
//		{
//			Name: "Invalid uid annotation missing slash or dash",
//			Namespace: func() *corev1.Namespace {
//				ns := &corev1.Namespace{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:      "default",
//						Namespace: "default",
//						Annotations: map[string]string{
//							// annotation should have a slash '/' or dash '-'
//							constants.AnnotationOpenShiftUIDRange: "5678",
//						},
//					},
//				}
//				return ns
//			},
//			Expected: 0,
//			Err: fmt.Sprintf(
//				"annotation %s contains an invalid format for value %s",
//				constants.AnnotationOpenShiftUIDRange,
//				"5678",
//			),
//		},
//		{
//			Name: "Missing uid annotation",
//			Namespace: func() *corev1.Namespace {
//				ns := &corev1.Namespace{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:      "default",
//						Namespace: "default",
//					},
//				}
//				return ns
//			},
//			Expected: 0,
//			Err:      fmt.Sprintf("unable to find annotation %s", constants.AnnotationOpenShiftUIDRange),
//		},
//		{
//			Name: "Empty",
//			Namespace: func() *corev1.Namespace {
//				ns := &corev1.Namespace{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:      "default",
//						Namespace: "default",
//						Annotations: map[string]string{
//							constants.AnnotationOpenShiftUIDRange: "",
//						},
//					},
//				}
//				return ns
//			},
//			Expected: 0,
//			Err:      "found annotation openshift.io/sa.scc.uid-range but it was empty",
//		},
//	}
//	for _, tt := range cases {
//		t.Run(tt.Name, func(t *testing.T) {
//			require := require.New(t)
//			actual, err := GetDataplaneUID(tt.Namespace(), tt.Pod)
//			if tt.Err == "" {
//				require.NoError(err)
//				require.Equal(tt.Expected, actual)
//			} else {
//				require.EqualError(err, tt.Err)
//			}
//		})
//	}
//}

func TestGetDataplaneIDs(t *testing.T) {
	cases := []struct {
		Name      string
		Namespace corev1.Namespace
		// User IDs and Group IDs are quite often the same, and will be for test purposes
		ExpectedDataplaneUserAndGroupIDs int64
		Pod                              corev1.Pod
		Err                              string
	}{
		{
			Name: "App using a single ID already",
			Namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationOpenShiftUIDRange: "100/5",
						constants.AnnotationOpenShiftGroups:   "100/5",
					},
				},
			},
			Pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod",
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "consul-connect-inject-init",
						},
					},
					Containers: []corev1.Container{
						{
							Name: "consul-dataplane",
						},
						{
							Name: "app",
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(100),
							},
						},
					},
				},
			},
			ExpectedDataplaneUserAndGroupIDs: 103,
			Err:                              "",
		},
		{
			Name: "Not enough available IDs",
			Namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationOpenShiftUIDRange: "100/2",
						constants.AnnotationOpenShiftGroups:   "100/2",
					},
				},
			},
			Pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod",
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "consul-connect-inject-init",
						},
					},
					Containers: []corev1.Container{
						{
							Name: "consul-dataplane",
						},
						{
							Name: "app",
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(100),
							},
						},
					},
				},
			},
			Err: "namespace does not have enough available UIDs",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			// Test UID
			actualUIDs, err := GetDataplaneUID(tt.Namespace, tt.Pod)
			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.ExpectedDataplaneUserAndGroupIDs, actualUIDs)
			} else {
				require.EqualError(err, tt.Err)
			}
			// Test GroupID
			actualGroupIDs, err := GetDataplaneGroupID(tt.Namespace, tt.Pod)
			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.ExpectedDataplaneUserAndGroupIDs, actualGroupIDs)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestGetAvailableIDs(t *testing.T) {
	cases := []struct {
		Name                      string
		Namespace                 corev1.Namespace
		ExpectedAvailableUserIDs  []int64
		ExpectedAvailableGroupIDs []int64
		Pod                       corev1.Pod
		Err                       string
	}{
		{
			Name: "App using a single ID already",
			Namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationOpenShiftUIDRange: "100/5",
						constants.AnnotationOpenShiftGroups:   "100/5",
					},
				},
			},
			Pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod",
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "consul-connect-inject-init",
						},
					},
					Containers: []corev1.Container{
						{
							Name: "consul-dataplane",
						},
						{
							Name: "app",
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(100),
							},
						},
					},
				},
			},
			ExpectedAvailableUserIDs:  []int64{101, 102, 103, 104},
			ExpectedAvailableGroupIDs: []int64{101, 102, 103, 104},
			Err:                       "",
		},
		{
			Name: "Bad annotation format",
			Namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationOpenShiftUIDRange: "100:5",
					},
				},
			},
			Pod:                      corev1.Pod{},
			ExpectedAvailableUserIDs: nil,
			Err:                      "unable to get valid userIDs from namespace annotation: invalid range format: 100:5",
		},
		{
			Name: "Group has multiple ranges",
			Namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationOpenShiftUIDRange: "100/5",
						constants.AnnotationOpenShiftGroups:   "100/5,200/5",
					},
				},
			},
			Pod:                       corev1.Pod{},
			ExpectedAvailableUserIDs:  []int64{100, 101, 102, 103, 104},
			ExpectedAvailableGroupIDs: []int64{100, 101, 102, 103, 104, 200, 201, 202, 203, 204},
			Err:                       "",
		},
		{
			Name: "Group is not defined and falls back to UID range annotation",
			Namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						constants.AnnotationOpenShiftUIDRange: "100/5",
					},
				},
			},
			Pod:                       corev1.Pod{},
			ExpectedAvailableUserIDs:  []int64{100, 101, 102, 103, 104},
			ExpectedAvailableGroupIDs: []int64{100, 101, 102, 103, 104},
			Err:                       "",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			actualUserIDs, err := getAvailableIDs(tt.Namespace, tt.Pod, constants.AnnotationOpenShiftUIDRange)
			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.ExpectedAvailableUserIDs, actualUserIDs)
			} else {
				require.EqualError(err, tt.Err)
			}
			actualGroupIDs, err := getAvailableIDs(tt.Namespace, tt.Pod, constants.AnnotationOpenShiftGroups)
			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.ExpectedAvailableGroupIDs, actualGroupIDs)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestGetIDsInRange(t *testing.T) {
	cases := []struct {
		Name               string
		Annotation         string
		ExpectedLen        int
		ExpectedFirstValue int64
		ExpectedLastValue  int64
		Err                string
	}{
		{
			Name:               "Valid uid annotation with slash",
			Annotation:         "1000700000/100000",
			ExpectedLen:        100000,
			ExpectedFirstValue: 1000700000,
			ExpectedLastValue:  1000799999,
			Err:                "",
		},
		{
			Name:               "Valid uid annotation with dash",
			Annotation:         "1234-1000",
			ExpectedLen:        1000,
			ExpectedFirstValue: 1234,
			ExpectedLastValue:  2233,
			Err:                "",
		},
		{
			Name:       "Invalid uid annotation missing slash or dash",
			Annotation: "5678",
			Err:        fmt.Sprintf("invalid range format: %s", "5678"),
		},
		{
			Name:       "Empty",
			Annotation: "",
			Err:        fmt.Sprintf("invalid range format: %s", ""),
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			actual, err := getIDsInRange(tt.Annotation)
			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.ExpectedLen, len(actual))
				require.Equal(tt.ExpectedFirstValue, actual[0])
				require.Equal(tt.ExpectedLastValue, actual[len(actual)-1])
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}
