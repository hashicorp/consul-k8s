package v1alpha1

// ConfigEntryStatus is the status of this custom resource.
// It is used by all the config entry CRDs.
type ConfigEntryStatus struct {
	Status `json:",inline"`
}
