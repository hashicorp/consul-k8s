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
	corev1 "k8s.io/api/core/v1"
)

// Get the user id from the OpenShift annotation 'openshift.io/sa.scc.uid-range'
func GetOpenShiftUID(ns *corev1.Namespace) (int64, error) {
	annotation, ok := ns.Annotations[constants.AnnotationOpenShiftUIDRange]
	if !ok {
		return 0, fmt.Errorf("unable to find annotation %s", constants.AnnotationOpenShiftUIDRange)
	}
	if len(annotation) == 0 {
		return 0, fmt.Errorf("found annotation %s but it was empty", constants.AnnotationOpenShiftUIDRange)
	}

	uid, err := parseOpenShiftUID(annotation)
	if err != nil {
		return 0, err
	}

	return uid, nil
}

// Parse the UID "range" from the annotation string. The annotation can either have a '/' or '-' as a separator.
// '-' is the old style of UID from when it used to be an actual range.
func parseOpenShiftUID(val string) (int64, error) {
	var uid int64
	var err error
	if strings.Contains(val, "/") {
		str := strings.Split(val, "/")
		uid, err = strconv.ParseInt(str[0], 10, 64)
		if err != nil {
			return 0, err
		}
	}
	if strings.Contains(val, "-") {
		str := strings.Split(val, "-")
		uid, err = strconv.ParseInt(str[0], 10, 64)
		if err != nil {
			return 0, err
		}
	}

	if !strings.Contains(val, "/") && !strings.Contains(val, "-") {
		return 0, fmt.Errorf(
			"annotation %s contains an invalid format for value %s",
			constants.AnnotationOpenShiftUIDRange,
			val,
		)
	}

	return uid, nil
}

// Get the user id from the OpenShift annotation 'openshift.io/sa.scc.supplemental-groups'
// Fall back to the UID annotation if the group annotation does not exist. The values should
// be the same.
func GetOpenShiftGroup(ns *corev1.Namespace) (int64, error) {
	annotation, ok := ns.Annotations[constants.AnnotationOpenShiftGroups]
	if !ok {
		// fall back to UID annotation
		annotation, ok = ns.Annotations[constants.AnnotationOpenShiftUIDRange]
		if !ok {
			return 0, fmt.Errorf(
				"unable to find annotation %s or %s",
				constants.AnnotationOpenShiftGroups,
				constants.AnnotationOpenShiftUIDRange,
			)
		}
	}
	if len(annotation) == 0 {
		return 0, fmt.Errorf("found annotation %s but it was empty", constants.AnnotationOpenShiftGroups)
	}

	uid, err := parseOpenShiftGroup(annotation)
	if err != nil {
		return 0, err
	}

	return uid, nil
}

// Parse the group from the annotation string. The annotation can either have a '/' or ',' as a separator.
// ',' is the old style of UID from when it used to be an actual range.
func parseOpenShiftGroup(val string) (int64, error) {
	var group int64
	var err error
	if strings.Contains(val, "/") {
		str := strings.Split(val, "/")
		group, err = strconv.ParseInt(str[0], 10, 64)
		if err != nil {
			return 0, err
		}
	}
	if strings.Contains(val, ",") {
		str := strings.Split(val, ",")
		group, err = strconv.ParseInt(str[0], 10, 64)
		if err != nil {
			return 0, err
		}
	}

	if !strings.Contains(val, "/") && !strings.Contains(val, ",") {
		return 0, fmt.Errorf("annotation %s contains an invalid format for value %s", constants.AnnotationOpenShiftGroups, val)
	}

	return group, nil
}
