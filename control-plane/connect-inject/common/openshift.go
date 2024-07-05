// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Function copied from:
// https://github.com/openshift/apiserver-library-go/blob/release-4.17/pkg/securitycontextconstraints/sccmatching/matcher.go
// Apache 2.0 license: https://github.com/openshift/apiserver-library-go/blob/release-4.17/LICENSE

// A namespace in OpenShift has the following annotations:
// Annotations:  openshift.io/sa.scc.mcs: s0:c27,c4
//               openshift.io/sa.scc.uid-range: 1000710000/10000
//               openshift.io/sa.scc.supplemental-groups: 1000710000/10000
//
// Note: Even though the annotation is named 'range', it is not a range but the ID you should use. All pods in a
// namespace should use the same UID/GID. (1000710000/1000710000 above)

package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
)

func GetDataplaneUID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, namespace.Annotations[constants.AnnotationOpenShiftUIDRange])
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-2], nil
}

func GetDataplaneGroupID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, namespace.Annotations[constants.AnnotationOpenShiftGroups])
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-2], nil
}

func GetConnectInitUID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, namespace.Annotations[constants.AnnotationOpenShiftUIDRange])
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 1 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-1], nil
}

func GetConnectInitGroupID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, namespace.Annotations[constants.AnnotationOpenShiftGroups])
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-1], nil
}

func getAvailableIDs(namespace corev1.Namespace, pod corev1.Pod, annotationName string) ([]int64, error) {
	// Collect the list of IDs designated in the Pod for application containers
	appUIDs := make([]int64, 0)
	if pod.Spec.SecurityContext != nil {
		if pod.Spec.SecurityContext.RunAsUser != nil {
			appUIDs = append(appUIDs, *pod.Spec.SecurityContext.RunAsUser)
		}
	}
	for _, c := range pod.Spec.Containers {
		if c.SecurityContext != nil && c.SecurityContext.RunAsUser != nil {
			appUIDs = append(appUIDs, *c.SecurityContext.RunAsUser)
		}
	}

	// Collect the list of valid UIDs from the namespace annotation
	validUIDs, err := GetAllValidUserIDsFromNamespace(namespace.Annotations[annotationName])
	if err != nil {
		return nil, fmt.Errorf("unable to get valid userIDs from namespace annotation: %w", err)
	}

	// Subtract the list of application container UIDs from the list of valid userIDs
	availableUIDs := make(map[int64]struct{})
	for _, uid := range validUIDs {
		availableUIDs[uid] = struct{}{}
	}
	for _, uid := range appUIDs {
		delete(availableUIDs, uid)
	}

	// Return the second to last (sorted) valid UID from the available UIDs
	keys := maps.Keys(availableUIDs)
	slices.Sort(keys)

	return keys, nil
}

type idSelector func(values []int64) (int64, error)

var SelectFirstInRange idSelector = func(values []int64) (int64, error) {
	if len(values) < 1 {
		return 0, fmt.Errorf("range must have at least 1 value")
	}
	return values[0], nil
}

var SelectSidecarID idSelector = func(values []int64) (int64, error) {
	if len(values) < 2 {
		return 0, fmt.Errorf("range must have at least 2 values")
	}
	return values[len(values)-2], nil
}

var SelectInitContainerID idSelector = func(values []int64) (int64, error) {
	if len(values) < 1 {
		return 0, fmt.Errorf("range must have at least 1 value")
	}
	return values[len(values)-1], nil
}

func GetAllValidUserIDsFromNamespace(annotation string) ([]int64, error) {
	parts := strings.Split(annotation, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range format: %s", annotation)
	}

	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid range format: %s", parts[0])
	}

	length, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid range format: %s", parts[1])
	}

	userIDs := make([]int64, length)
	for i := 0; i < length; i++ {
		userIDs[i] = int64(start + i)
	}

	return userIDs, nil
}
