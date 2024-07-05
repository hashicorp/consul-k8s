// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func GetDataplaneUID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftUIDRange)
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-2], nil
}

func GetDataplaneGroupID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftGroups)
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-2], nil
}

func GetConnectInitUID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftUIDRange)
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 1 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-1], nil
}

func GetConnectInitGroupID(namespace corev1.Namespace, pod corev1.Pod) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftGroups)
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
		if strings.HasPrefix(c.Name, "consul-dataplane") {
			continue
		}

		if strings.HasPrefix(c.Name, "consul-connect-inject-init") {
			continue
		}

		if c.SecurityContext != nil && c.SecurityContext.RunAsUser != nil {
			appUIDs = append(appUIDs, *c.SecurityContext.RunAsUser)
		}
	}

	// Collect the list of valid IDs from the namespace annotation
	validUIDs, err := getIDsInRange(namespace.Annotations[annotationName])
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

func getIDsInRange(annotation string) ([]int64, error) {
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
