# This Dockerfile contains multiple targets.
# Use 'docker build --target=<name> .' to build one.
#
# Every target has a BIN_NAME argument that must be provided via --build-arg=BIN_NAME=<name>
# when building.


# ===================================
# 
#   Non-release images.
#
# ===================================

# dev copies the binary from a local build
# -----------------------------------
# BIN_NAME is a requirement in the hashicorp docker github action 

FROM alpine:3.16 AS dev

# NAME and VERSION are the name of the software in releases.hashicorp.com
# and the version to download. Example: NAME=consul VERSION=1.2.3.
ARG BIN_NAME=consul-k8s-control-plane
ARG CNI_BIN_NAME=consul-cni
ARG VERSION
ARG TARGETARCH
ARG TARGETOS

LABEL name=${BIN_NAME} \
      maintainer="Team Consul Kubernetes <team-consul-kubernetes@hashicorp.com>" \
      vendor="HashiCorp" \
      version=${VERSION} \
      release=${VERSION} \
      summary="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      description="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes."

# Set ARGs as ENV so that they can be used in ENTRYPOINT/CMD
ENV BIN_NAME=${BIN_NAME}
ENV VERSION=${VERSION}

RUN apk add --no-cache ca-certificates curl gnupg libcap openssl su-exec iputils libc6-compat iptables

# Create a non-root user to run the software.
RUN addgroup ${BIN_NAME} && \
    adduser -S -G ${BIN_NAME} 100

COPY pkg/bin/linux_${TARGETARCH}/${BIN_NAME} /bin
COPY cni/pkg/bin/linux_${TARGETARCH}/${CNI_BIN_NAME} /bin

USER 100
CMD /bin/${BIN_NAME}


# ===================================
# 
#   Release images.
#
# ===================================


# default release image
# -----------------------------------
# This Dockerfile creates a production release image for the project. This
# downloads the release from releases.hashicorp.com and therefore requires that
# the release is published before building the Docker image.
#
# We don't rebuild the software because we want the exact checksums and
# binary signatures to match the software and our builds aren't fully
# reproducible currently.
FROM alpine:3.16 AS release-default

# NAME and VERSION are the name of the software in releases.hashicorp.com
# and the version to download. Example: NAME=consul VERSION=1.2.3.
ARG BIN_NAME=consul-k8s-control-plane
ARG CNI_BIN_NAME=consul-cni
ARG VERSION

LABEL name=${BIN_NAME} \
      maintainer="Team Consul Kubernetes <team-consul-kubernetes@hashicorp.com>" \
      vendor="HashiCorp" \
      version=${VERSION} \
      release=${VERSION} \
      summary="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      description="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes."

# Set ARGs as ENV so that they can be used in ENTRYPOINT/CMD
ENV BIN_NAME=${BIN_NAME}
ENV VERSION=${VERSION}

RUN apk add --no-cache ca-certificates curl gnupg libcap openssl su-exec iputils libc6-compat iptables

# TARGETOS and TARGETARCH are set automatically when --platform is provided.
ARG TARGETOS
ARG TARGETARCH

# Create a non-root user to run the software.
RUN addgroup ${BIN_NAME} && \
    adduser -S -G ${BIN_NAME} 100

COPY dist/${TARGETOS}/${TARGETARCH}/${BIN_NAME} /bin/
COPY cni/dist/${TARGETOS}/${TARGETARCH}/${CNI_BIN_NAME} /bin/

USER 100
CMD /bin/${BIN_NAME}

# -----------------------------------
# Dockerfile target for consul-k8s with UBI as its base image. Used for running on
# OpenShift.
#
# This Dockerfile creates a production release image for the project. This
# downloads the release from releases.hashicorp.com and therefore requires that
# the release is published before building the Docker image.
#
# We don't rebuild the software because we want the exact checksums and
# binary signatures to match the software and our builds aren't fully
# reproducible currently.
FROM registry.access.redhat.com/ubi8/ubi-minimal:8.6 as ubi

ARG PRODUCT_NAME
ARG PRODUCT_VERSION
ARG PRODUCT_REVISION
ARG BIN_NAME
ARG CNI_BIN_NAME=consul-cni
ARG VERSION

# PRODUCT_NAME and PRODUCT_VERSION are the name of the software on releases.hashicorp.com
# and the version to download. Example: PRODUCT_NAME=consul PRODUCT_VERSION=1.2.3.
ENV BIN_NAME=$BIN_NAME
ENV PRODUCT_VERSION=$PRODUCT_VERSION

ARG PRODUCT_NAME=$BIN_NAME

LABEL name=$PRODUCT_NAME \
      maintainer="Team Consul Kubernetes <team-consul-kubernetes@hashicorp.com>" \
      vendor="HashiCorp" \
      version=$PRODUCT_VERSION \
      release=$PRODUCT_VERSION \
      summary="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      description="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes."

# Set ARGs as ENV so that they can be used in ENTRYPOINT/CMD
ENV NAME=${BIN_NAME}
ENV VERSION=${VERSION}

# TARGETOS and TARGETARCH are set automatically when --platform is provided.
ARG TARGETOS
ARG TARGETARCH

# Copy license for Red Hat certification.
COPY LICENSE.md /licenses/mozilla.txt

RUN microdnf install -y ca-certificates gnupg libcap openssl shadow-utils iptables

# Create a non-root user to run the software. On OpenShift, this
# will not matter since the container is run as a random user and group
# but this is kept for consistency with our other images.
RUN groupadd --gid 1000 ${BIN_NAME} && \
    adduser --uid 100 --system -g ${BIN_NAME} ${BIN_NAME} && \
    usermod -a -G root ${BIN_NAME}

COPY dist/${TARGETOS}/${TARGETARCH}/${BIN_NAME} /bin/
COPY cni/dist/${TARGETOS}/${TARGETARCH}/${CNI_BIN_NAME} /bin/

USER 100
CMD /bin/${BIN_NAME}

# ===================================
# 
#   Set default target to 'dev'.
#
# ===================================
FROM dev
