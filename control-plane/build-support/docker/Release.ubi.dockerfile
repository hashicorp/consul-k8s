# Dockerfile for consul-k8s with UBI as its base image. Used for running on
# OpenShift.
#
# This Dockerfile creates a production release image for the project. This
# downloads the release from releases.hashicorp.com and therefore requires that
# the release is published before building the Docker image.
#
# We don't rebuild the software because we want the exact checksums and
# binary signatures to match the software and our builds aren't fully
# reproducible currently.
FROM registry.access.redhat.com/ubi8/ubi-minimal:8.4

# NAME and VERSION are the name of the software in releases.hashicorp.com
# and the version to download. Example: NAME=consul VERSION=1.2.3.
ARG NAME
ARG VERSION

LABEL name=$NAME \
      maintainer="Consul Team <consul@hashicorp.com>" \
      vendor="HashiCorp" \
      version=$VERSION \
      release=$VERSION \
      summary="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes." \
      description="consul-k8s-control-plane provides first-class integrations between Consul and Kubernetes."

# Set ARGs as ENV so that they can be used in ENTRYPOINT/CMD
ENV NAME=$NAME
ENV VERSION=$VERSION

# This is the location of the releases.
ENV HASHICORP_RELEASES=https://releases.hashicorp.com

# Copy license for Red Hat certification.
COPY LICENSE.md /licenses/mozilla.txt

# Set up certificates, base tools, and software.
RUN set -eux && \
    microdnf install -y ca-certificates curl gnupg libcap openssl wget unzip tar shadow-utils iptables && \
    BUILD_GPGKEY=C874011F0AB405110D02105534365D9472D7468F; \
    found=''; \
    for server in \
        hkp://p80.pool.sks-keyservers.net:80 \
        hkp://keyserver.ubuntu.com:80 \
        hkp://pgp.mit.edu:80 \
    ; do \
        echo "Fetching GPG key $BUILD_GPGKEY from $server"; \
        gpg --keyserver "$server" --recv-keys "$BUILD_GPGKEY" && found=yes && break; \
    done; \
    test -z "$found" && echo >&2 "error: failed to fetch GPG key $BUILD_GPGKEY" && exit 1; \
    mkdir -p /tmp/build && \
    cd /tmp/build && \
    ARCH=amd64 && \
    wget ${HASHICORP_RELEASES}/${NAME}/${VERSION}/${NAME}_${VERSION}_linux_${ARCH}.zip && \
    wget ${HASHICORP_RELEASES}/${NAME}/${VERSION}/${NAME}_${VERSION}_SHA256SUMS && \
    wget ${HASHICORP_RELEASES}/${NAME}/${VERSION}/${NAME}_${VERSION}_SHA256SUMS.sig && \
    gpg --batch --verify ${NAME}_${VERSION}_SHA256SUMS.sig ${NAME}_${VERSION}_SHA256SUMS && \
    grep ${NAME}_${VERSION}_linux_${ARCH}.zip ${NAME}_${VERSION}_SHA256SUMS | sha256sum -c && \
    unzip -d /bin ${NAME}_${VERSION}_linux_${ARCH}.zip && \
    cd /tmp && \
    rm -rf /tmp/build && \
    gpgconf --kill all && \
    rm -rf /root/.gnupg

# Create a non-root user to run the software. On OpenShift, this
# will not matter since the container is run as a random user and group
# but this is kept for consistency with our other images.
RUN groupadd --gid 1000 ${NAME} && \
    adduser --uid 100 --system -g ${NAME} ${NAME} && \
    usermod -a -G root ${NAME}

USER 100
CMD /bin/${NAME}
