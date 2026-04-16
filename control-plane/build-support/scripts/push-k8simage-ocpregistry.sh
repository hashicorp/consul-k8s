#!/bin/bash
# Optional parameter: OCP_REGISTRY_ROUTE - the route to the OCP image registry.
# If not provided, the script enables the default registry route and resolves it with `oc`.
set -euo pipefail

ensure_oc_login() {
	if ! oc whoami >/dev/null 2>&1; then
		echo "OpenShift login is required. Run 'oc login' and try again." >&2
		exit 1
	fi

	if ! oc get --raw=/apis >/dev/null 2>&1; then
		echo "Unable to reach the current OpenShift cluster. Verify your active oc context and DNS/network access, then try again." >&2
		exit 1
	fi
}

discover_ocp_registry_route() {
	local route_host

	echo "Ensuring the OpenShift image registry default route is enabled..." >&2
	oc patch configs.imageregistry.operator.openshift.io/cluster --type=merge -p '{"spec":{"defaultRoute":true}}' >/dev/null

	route_host="$(oc get route default-route -n openshift-image-registry -o jsonpath='{.spec.host}' 2>/dev/null || true)"
	if [[ -z "${route_host}" ]]; then
		route_host="$(oc get routes -n openshift-image-registry -o jsonpath='{.items[0].spec.host}' 2>/dev/null || true)"
	fi

	printf '%s' "${route_host}"
}

ensure_oc_login

OCP_REGISTRY_ROUTE="${OCP_REGISTRY_ROUTE:-}"
if [[ -z "${OCP_REGISTRY_ROUTE}" ]]; then
	OCP_REGISTRY_ROUTE="$(discover_ocp_registry_route)"
fi

if [[ -z "${OCP_REGISTRY_ROUTE}" ]]; then
	read -r -p "Enter the OCP image registry route (for example, default-route-openshift-image-registry.apps.<cluster-domain>): " OCP_REGISTRY_ROUTE
fi

if [[ -z "${OCP_REGISTRY_ROUTE}" ]]; then
	echo "Failed to determine OCP registry route. Exiting."
	exit 1
fi

if [[ "${OCP_REGISTRY_ROUTE}" =~ [[:space:]] ]]; then
	echo "Resolved OCP registry route is invalid: ${OCP_REGISTRY_ROUTE}" >&2
	exit 1
fi

echo "Using OCP registry route: ${OCP_REGISTRY_ROUTE}"

# this script will generate the local image for ocp and load it
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTROL_PLANE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

IMAGE_TAG="8.4"
IMAGE_NAME="consul-k8s-control-plane:${IMAGE_TAG}"
LOCAL_BUILD_IMAGE="consul-k8s-control-plane:amd64"
GO_DISCOVER_BUILD_VERSION="${GO_DISCOVER_BUILD_VERSION:-1.25.7}"

echo "Building local linux/amd64 binaries from current source..."
mkdir -p "${CONTROL_PLANE_DIR}/pkg/bin/linux_amd64" "${CONTROL_PLANE_DIR}/cni/pkg/bin/linux_amd64"
rm -f "${CONTROL_PLANE_DIR}/pkg/bin/linux_amd64/consul-k8s-control-plane"
rm -f "${CONTROL_PLANE_DIR}/cni/pkg/bin/linux_amd64/consul-cni"

if ! (
	cd "${CONTROL_PLANE_DIR}" && \
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -a -o pkg/bin/linux_amd64/consul-k8s-control-plane .
); then
	echo "Failed to build consul-k8s-control-plane binary. Exiting."
	exit 1
fi

if ! (
	cd "${CONTROL_PLANE_DIR}/cni" && \
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -a -o pkg/bin/linux_amd64/consul-cni .
); then
	echo "Failed to build consul-cni binary. Exiting."
	exit 1
fi

if ! docker buildx build \
	--platform linux/amd64 \
	--no-cache \
	--target dev \
	--build-arg GOLANG_VERSION="${GO_DISCOVER_BUILD_VERSION}" \
	--build-arg BIN_NAME=consul-k8s-control-plane \
	--build-arg VERSION=latest \
	-t "${LOCAL_BUILD_IMAGE}" \
	-f "${CONTROL_PLANE_DIR}/Dockerfile" \
	"${CONTROL_PLANE_DIR}" \
	--load; then
	echo "Failed to build the image. Exiting."
	exit 1
fi

# import the docker-built image into local podman
if ! docker save "${LOCAL_BUILD_IMAGE}" | podman load; then
	echo "Failed to import docker image into podman. Exiting."
	exit 1
fi

# login
if ! podman login -u "$(oc whoami)" -p "$(oc whoami -t)" "${OCP_REGISTRY_ROUTE}"; then
    echo "Failed to login to OCP registry. Exiting."
    exit 1
fi

# tag the image for ocp
podman tag "${LOCAL_BUILD_IMAGE}" "${IMAGE_NAME}"

# tag the image for ocp registry
podman tag "${IMAGE_NAME}" "${OCP_REGISTRY_ROUTE}/openshift/${IMAGE_NAME}"

# push the image to ocp registry
podman push "${OCP_REGISTRY_ROUTE}/openshift/${IMAGE_NAME}"

echo "Pushed image: ${OCP_REGISTRY_ROUTE}/openshift/${IMAGE_NAME}"