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

// GetDataplaneUID returns the UID to use for the Dataplane container in the given namespace.
// The UID is based on the namespace annotation and avoids conflicting with any application container UIDs.
// Containers with dataplaneImage and k8sImage are not considered application containers.
func GetDataplaneUID(namespace corev1.Namespace, pod corev1.Pod, dataplaneImage, k8sImage string) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftUIDRange, dataplaneImage, k8sImage)
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-2], nil
}

// GetDataplaneGroupID returns the group ID to use for the Dataplane container in the given namespace.
// The UID is based on the namespace annotation and avoids conflicting with any application container group IDs.
// Containers with dataplaneImage and k8sImage are not considered application containers.
func GetDataplaneGroupID(namespace corev1.Namespace, pod corev1.Pod, dataplaneImage, k8sImage string) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftGroups, dataplaneImage, k8sImage)
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-2], nil
}

// GetConnectInitUID returns the UID to use for the connect init container in the given namespace.
// The UID is based on the namespace annotation and avoids conflicting with any application container UIDs.
// Containers with dataplaneImage and k8sImage are not considered application containers.
func GetConnectInitUID(namespace corev1.Namespace, pod corev1.Pod, dataplaneImage, k8sImage string) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftUIDRange, dataplaneImage, k8sImage)
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 1 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-1], nil
}

// GetConnectInitGroupID returns the group ID to use for the connect init container in the given namespace.
// The group ID is based on the namespace annotation and avoids conflicting with any application container group IDs.
// Containers with dataplaneImage and k8sImage are not considered application containers.
func GetConnectInitGroupID(namespace corev1.Namespace, pod corev1.Pod, dataplaneImage, k8sImage string) (int64, error) {
	availableUIDs, err := getAvailableIDs(namespace, pod, constants.AnnotationOpenShiftGroups, dataplaneImage, k8sImage)
	if err != nil {
		return 0, err
	}

	if len(availableUIDs) < 2 {
		return 0, fmt.Errorf("namespace does not have enough available UIDs")
	}

	return availableUIDs[len(availableUIDs)-1], nil
}

// getAvailableIDs enumerates the entire list of available UIDs in the namespace based on the
// OpenShift annotationName provided. It then removes the UIDs that are already in use by application
// containers. Containers with dataplaneImage and k8sImage are not considered application containers.
func getAvailableIDs(namespace corev1.Namespace, pod corev1.Pod, annotationName, dataplaneImage, k8sImage string) ([]int64, error) {
	// Collect the list of IDs designated in the Pod for application containers
	appUIDs := make([]int64, 0)
	if pod.Spec.SecurityContext != nil {
		if pod.Spec.SecurityContext.RunAsUser != nil {
			appUIDs = append(appUIDs, *pod.Spec.SecurityContext.RunAsUser)
		}
	}
	for _, c := range pod.Spec.Containers {
		if c.Image == dataplaneImage || c.Image == k8sImage {
			continue
		}

		if c.SecurityContext != nil && c.SecurityContext.RunAsUser != nil {
			appUIDs = append(appUIDs, *c.SecurityContext.RunAsUser)
		}
	}

	annotationValue := namespace.Annotations[annotationName]

	// Groups can be comma separated ranges, i.e. 100/2,101/2
	// https://docs.openshift.com/container-platform/4.16/authentication/managing-security-context-constraints.html#security-context-constraints-pre-allocated-values_configuring-internal-oauth
	ranges := make([]string, 0)
	validIDs := make([]int64, 0)
	// Collect the list of valid IDs from the namespace annotation
	if annotationName == constants.AnnotationOpenShiftGroups {
		// Fall back to UID range if Group annotation is not present
		if annotationValue == "" {
			annotationName = constants.AnnotationOpenShiftUIDRange
			annotationValue = namespace.Annotations[annotationName]
		}
		ranges = strings.Split(annotationValue, ",")
	} else {
		ranges = append(ranges, annotationValue)
	}

	for _, r := range ranges {
		rangeIDs, err := getIDsInRange(r)
		// call based on length of ranges and merge for groups
		if err != nil {
			return nil, fmt.Errorf("unable to get valid userIDs from namespace annotation: %w", err)
		}
		validIDs = append(validIDs, rangeIDs...)
	}

	// Subtract the list of application container UIDs from the list of valid userIDs
	availableUIDs := make(map[int64]struct{})
	for _, uid := range validIDs {
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

// getIDsInRange enumerates the entire list of available IDs given the value of the
// OpenShift annotation. This can be the group or user ID range.
func getIDsInRange(annotation string) ([]int64, error) {
	// Add comma and group fallback
	parts := strings.Split(annotation, "/")
	if len(parts) != 2 {
		parts = strings.Split(annotation, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range format: %s", annotation)
		}
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
