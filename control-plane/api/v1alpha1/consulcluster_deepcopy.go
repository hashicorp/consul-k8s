// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func (in *ConsulClusterPVCRetentionPolicy) DeepCopyInto(out *ConsulClusterPVCRetentionPolicy) {
	*out = *in
}

func (in *ConsulClusterPVCRetentionPolicy) DeepCopy() *ConsulClusterPVCRetentionPolicy {
	if in == nil {
		return nil
	}
	out := new(ConsulClusterPVCRetentionPolicy)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulTLSSpec) DeepCopyInto(out *ConsulTLSSpec) {
	*out = *in
}

func (in *ConsulTLSSpec) DeepCopy() *ConsulTLSSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulTLSSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulGossipSpec) DeepCopyInto(out *ConsulGossipSpec) {
	*out = *in
}

func (in *ConsulGossipSpec) DeepCopy() *ConsulGossipSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulGossipSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulSecretRef) DeepCopyInto(out *ConsulSecretRef) {
	*out = *in
}

func (in *ConsulSecretRef) DeepCopy() *ConsulSecretRef {
	if in == nil {
		return nil
	}
	out := new(ConsulSecretRef)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulACLSpec) DeepCopyInto(out *ConsulACLSpec) {
	*out = *in
	if in.Token != nil {
		in, out := &in.Token, &out.Token
		*out = new(ConsulSecretRef)
		**out = **in
	}
}

func (in *ConsulACLSpec) DeepCopy() *ConsulACLSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulACLSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulMetricsSpec) DeepCopyInto(out *ConsulMetricsSpec) {
	*out = *in
}

func (in *ConsulMetricsSpec) DeepCopy() *ConsulMetricsSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulMetricsSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulRequestLimitsSpec) DeepCopyInto(out *ConsulRequestLimitsSpec) {
	*out = *in
}

func (in *ConsulRequestLimitsSpec) DeepCopy() *ConsulRequestLimitsSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulRequestLimitsSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulLimitsSpec) DeepCopyInto(out *ConsulLimitsSpec) {
	*out = *in
	if in.RequestLimits != nil {
		v := *in.RequestLimits
		out.RequestLimits = &v
	}
}

func (in *ConsulLimitsSpec) DeepCopy() *ConsulLimitsSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulLimitsSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulPodDisruptionBudgetSpec) DeepCopyInto(out *ConsulPodDisruptionBudgetSpec) {
	*out = *in
	if in.MaxUnavailable != nil {
		v := *in.MaxUnavailable
		out.MaxUnavailable = &v
	}
}

func (in *ConsulPodDisruptionBudgetSpec) DeepCopy() *ConsulPodDisruptionBudgetSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulPodDisruptionBudgetSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulPodPolicy) DeepCopyInto(out *ConsulPodPolicy) {
	*out = *in
	if in.Annotations != nil {
		out.Annotations = make(map[string]string, len(in.Annotations))
		for k, v := range in.Annotations {
			out.Annotations[k] = v
		}
	}
	if in.Labels != nil {
		out.Labels = make(map[string]string, len(in.Labels))
		for k, v := range in.Labels {
			out.Labels[k] = v
		}
	}
	if in.NodeSelector != nil {
		out.NodeSelector = make(map[string]string, len(in.NodeSelector))
		for k, v := range in.NodeSelector {
			out.NodeSelector[k] = v
		}
	}
	if in.Affinity != nil {
		in, out := &in.Affinity, &out.Affinity
		*out = new(corev1.Affinity)
		(*in).DeepCopyInto(*out)
	}
	if in.Tolerations != nil {
		out.Tolerations = make([]corev1.Toleration, len(in.Tolerations))
		for i := range in.Tolerations {
			in.Tolerations[i].DeepCopyInto(&out.Tolerations[i])
		}
	}
	if in.TopologySpreadConstraints != nil {
		out.TopologySpreadConstraints = make([]corev1.TopologySpreadConstraint, len(in.TopologySpreadConstraints))
		for i := range in.TopologySpreadConstraints {
			in.TopologySpreadConstraints[i].DeepCopyInto(&out.TopologySpreadConstraints[i])
		}
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(corev1.ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.SecurityContext != nil {
		in, out := &in.SecurityContext, &out.SecurityContext
		*out = new(corev1.PodSecurityContext)
		(*in).DeepCopyInto(*out)
	}
	if in.ContainerSecurityContext != nil {
		in, out := &in.ContainerSecurityContext, &out.ContainerSecurityContext
		*out = new(corev1.SecurityContext)
		(*in).DeepCopyInto(*out)
	}
	if in.ExtraEnvVars != nil {
		out.ExtraEnvVars = make([]corev1.EnvVar, len(in.ExtraEnvVars))
		for i := range in.ExtraEnvVars {
			in.ExtraEnvVars[i].DeepCopyInto(&out.ExtraEnvVars[i])
		}
	}
	if in.ExtraVolumes != nil {
		out.ExtraVolumes = make([]corev1.Volume, len(in.ExtraVolumes))
		for i := range in.ExtraVolumes {
			in.ExtraVolumes[i].DeepCopyInto(&out.ExtraVolumes[i])
		}
	}
	if in.ExtraVolumeMounts != nil {
		out.ExtraVolumeMounts = make([]corev1.VolumeMount, len(in.ExtraVolumeMounts))
		for i := range in.ExtraVolumeMounts {
			in.ExtraVolumeMounts[i].DeepCopyInto(&out.ExtraVolumeMounts[i])
		}
	}
	if in.ExtraContainers != nil {
		out.ExtraContainers = make([]corev1.Container, len(in.ExtraContainers))
		for i := range in.ExtraContainers {
			in.ExtraContainers[i].DeepCopyInto(&out.ExtraContainers[i])
		}
	}
	if in.ImagePullSecrets != nil {
		out.ImagePullSecrets = make([]corev1.LocalObjectReference, len(in.ImagePullSecrets))
		copy(out.ImagePullSecrets, in.ImagePullSecrets)
	}
}

func (in *ConsulPodPolicy) DeepCopy() *ConsulPodPolicy {
	if in == nil {
		return nil
	}
	out := new(ConsulPodPolicy)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulClusterSpec) DeepCopyInto(out *ConsulClusterSpec) {
	*out = *in
	out.Storage = in.Storage.DeepCopy()
	if in.BootstrapExpect != nil {
		v := *in.BootstrapExpect
		out.BootstrapExpect = &v
	}
	if in.Pod != nil {
		in, out := &in.Pod, &out.Pod
		*out = new(ConsulPodPolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.StorageClassName != nil {
		v := *in.StorageClassName
		out.StorageClassName = &v
	}
	if in.PersistentVolumeClaimRetentionPolicy != nil {
		in, out := &in.PersistentVolumeClaimRetentionPolicy, &out.PersistentVolumeClaimRetentionPolicy
		*out = new(ConsulClusterPVCRetentionPolicy)
		**out = **in
	}
	if in.TLS != nil {
		in, out := &in.TLS, &out.TLS
		*out = new(ConsulTLSSpec)
		**out = **in
	}
	if in.GossipEncryption != nil {
		in, out := &in.GossipEncryption, &out.GossipEncryption
		*out = new(ConsulGossipSpec)
		**out = **in
	}
	if in.ACLs != nil {
		in, out := &in.ACLs, &out.ACLs
		*out = new(ConsulACLSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Recursors != nil {
		out.Recursors = make([]string, len(in.Recursors))
		copy(out.Recursors, in.Recursors)
	}
	if in.Metrics != nil {
		in, out := &in.Metrics, &out.Metrics
		*out = new(ConsulMetricsSpec)
		**out = **in
	}
	if in.Limits != nil {
		in, out := &in.Limits, &out.Limits
		*out = new(ConsulLimitsSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.PodDisruptionBudget != nil {
		in, out := &in.PodDisruptionBudget, &out.PodDisruptionBudget
		*out = new(ConsulPodDisruptionBudgetSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.ServiceAnnotations != nil {
		out.ServiceAnnotations = make(map[string]string, len(in.ServiceAnnotations))
		for k, v := range in.ServiceAnnotations {
			out.ServiceAnnotations[k] = v
		}
	}
	if in.ServiceAccountAnnotations != nil {
		out.ServiceAccountAnnotations = make(map[string]string, len(in.ServiceAccountAnnotations))
		for k, v := range in.ServiceAccountAnnotations {
			out.ServiceAccountAnnotations[k] = v
		}
	}
}

func (in *ConsulClusterSpec) DeepCopy() *ConsulClusterSpec {
	if in == nil {
		return nil
	}
	out := new(ConsulClusterSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulClusterStatus) DeepCopyInto(out *ConsulClusterStatus) {
	*out = *in
	if in.Members != nil {
		out.Members = make([]string, len(in.Members))
		copy(out.Members, in.Members)
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *ConsulClusterStatus) DeepCopy() *ConsulClusterStatus {
	if in == nil {
		return nil
	}
	out := new(ConsulClusterStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulCluster) DeepCopyInto(out *ConsulCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ConsulCluster) DeepCopy() *ConsulCluster {
	if in == nil {
		return nil
	}
	out := new(ConsulCluster)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *ConsulClusterList) DeepCopyInto(out *ConsulClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ConsulCluster, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ConsulClusterList) DeepCopy() *ConsulClusterList {
	if in == nil {
		return nil
	}
	out := new(ConsulClusterList)
	in.DeepCopyInto(out)
	return out
}

func (in *ConsulClusterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
