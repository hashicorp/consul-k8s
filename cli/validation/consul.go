package validation

import "strings"

// IsConsulImage checks if the image is a Consul image.
func IsConsulImage(image string) bool {
	return strings.HasPrefix(image, "hashicorp/consul")
}

func IsConsulEnterpriseImage(image string) bool {
	return strings.HasPrefix(image, "hashicorp/consul-enterprise") && strings.HasSuffix(image, "-ent")
}
