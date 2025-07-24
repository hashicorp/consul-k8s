# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

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

# go-discover builds the discover binary (which we don't currently publish
# either).
ARG GOLANG_VERSION
FROM golang:${GOLANG_VERSION}-alpine3.22 AS go-discover
RUN CGO_ENABLED=0 go install github.com/hashicorp/go-discover/cmd/discover@275a71457aa412bf20df9f9b77c380667164a5e6

# dev copies the binary from a local build
# -----------------------------------
# BIN_NAME is a requirement in the hashicorp docker github action 
FROM alpine:3.22 AS dev

# NAME and VERSION are the name of the software in releases.hashicorp.com
# and the version to download. Example: NAME=consul VERSION=1.2.3.
ARG BIN_NAME=consul-k8s-control-plane
ARG CNI_BIN_NAME=consul-cni
ARG VERSION
ARG TARGETARCH
ARG TARGETOS
ENV PRODUCT_NAME=$BIN_NAME

LABEL name=${BIN_NAME} \
      maintainer="Team Consul Kubernetes <team-consul-kubernetes@hashicorp.com>" \
      vendor="HashiCorp" \
      version=${VERSION} \
      release=${VERSION} \
      summary="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      description="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      org.opencontainers.image.licenses="MPL-2.0"

COPY LICENSE /usr/share/doc/$PRODUCT_NAME/LICENSE.txt

# Set ARGs as ENV so that they can be used in ENTRYPOINT/CMD
ENV BIN_NAME=${BIN_NAME}
ENV VERSION=${VERSION}

RUN apk add --no-cache ca-certificates libcap openssl su-exec iputils libc6-compat iptables

# Create a non-root user to run the software.
RUN addgroup ${BIN_NAME} && \
    adduser -S -G ${BIN_NAME} 100

COPY --from=go-discover /go/bin/discover /bin/
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
FROM alpine:3.22 AS release-default

ARG BIN_NAME=consul-k8s-control-plane
ARG CNI_BIN_NAME=consul-cni
ARG PRODUCT_VERSION
ENV PRODUCT_NAME=$BIN_NAME

LABEL name=${BIN_NAME} \
      maintainer="Team Consul Kubernetes <team-consul-kubernetes@hashicorp.com>" \
      vendor="HashiCorp" \
      version=${PRODUCT_VERSION} \
      release=${PRODUCT_VERSION} \
      summary="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      description="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      org.opencontainers.image.licenses="MPL-2.0"

COPY LICENSE /usr/share/doc/$PRODUCT_NAME/LICENSE.txt

# Set ARGs as ENV so that they can be used in ENTRYPOINT/CMD
ENV BIN_NAME=${BIN_NAME}
ENV VERSION=${PRODUCT_VERSION}

RUN apk add --no-cache ca-certificates libcap openssl su-exec iputils gcompat libc6-compat libstdc++ iptables

# for FIPS CGO glibc compatibility in alpine
# see https://github.com/golang/go/issues/59305
RUN ln -s /lib/libc.so.6 /usr/lib/libresolv.so.2

# TARGETOS and TARGETARCH are set automatically when --platform is provided.
ARG TARGETOS
ARG TARGETARCH

# Create a non-root user to run the software.
RUN addgroup ${BIN_NAME} && \
    adduser -S -G ${BIN_NAME} 100

COPY --from=go-discover /go/bin/discover /bin/
COPY dist/${TARGETOS}/${TARGETARCH}/${BIN_NAME} /bin/
COPY dist/cni/${TARGETOS}/${TARGETARCH}/${CNI_BIN_NAME} /bin/

USER 100
CMD /bin/${BIN_NAME}

# Duplicate target for FIPS builds
FROM release-default AS release-default-fips

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
FROM registry.access.redhat.com/ubi9-minimal:9.6-1752069876 AS ubi

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
ENV PRODUCT_NAME=$BIN_NAME

LABEL name=$PRODUCT_NAME \
      maintainer="Team Consul Kubernetes <team-consul-kubernetes@hashicorp.com>" \
      vendor="HashiCorp" \
      version=$PRODUCT_VERSION \
      release=$PRODUCT_VERSION \
      summary="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      description="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      org.opencontainers.image.licenses="MPL-2.0"

COPY LICENSE /usr/share/doc/$PRODUCT_NAME/LICENSE.txt

# Set ARGs as ENV so that they can be used in ENTRYPOINT/CMD
ENV NAME=${BIN_NAME}
ENV VERSION=${PRODUCT_VERSION}

# TARGETOS and TARGETARCH are set automatically when --platform is provided.
ARG TARGETOS
ARG TARGETARCH

# Copy license for Red Hat certification.
COPY LICENSE /licenses/mozilla.txt

RUN microdnf install -y ca-certificates libcap openssl shadow-utils iptables

# Create a non-root user to run the software. On OpenShift, this
# will not matter since the container is run as a random user and group
# but this is kept for consistency with our other images.
RUN groupadd --gid 1000 ${BIN_NAME} && \
    adduser --uid 100 --system -g ${BIN_NAME} ${BIN_NAME} && \
    usermod -a -G root ${BIN_NAME}

COPY --from=go-discover /go/bin/discover /bin/
COPY dist/${TARGETOS}/${TARGETARCH}/${BIN_NAME} /bin/
COPY dist/cni/${TARGETOS}/${TARGETARCH}/${CNI_BIN_NAME} /bin/

USER 100
CMD /bin/${BIN_NAME}

# Duplicate target for FIPS builds
FROM ubi AS ubi-fips
# ===================================
# 
#   Set default target to 'dev'.
#
# ===================================
FROM dev
