# This Dockerfile creates a production release image for the project. This
# downloads the release from releases.hashicorp.com and therefore requires that
# the release is published before building the Docker image.
#
# We don't rebuild the software because we want the exact checksums and
# binary signatures to match the software and our builds aren't fully
# reproducible currently.
FROM alpine:3.15

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

# Create a non-root user to run the software.
RUN addgroup ${NAME} && \
    adduser -S -G ${NAME} 100

# Set up certificates, base tools, and software.
RUN set -eux && \
    apk add --no-cache ca-certificates curl gnupg libcap openssl su-exec iputils libc6-compat iptables && \
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
    apkArch="$(apk --print-arch)" && \
    case "${apkArch}" in \
    aarch64) ARCH='arm64' ;; \
    armhf) ARCH='arm' ;; \
    x86) ARCH='386' ;; \
    x86_64) ARCH='amd64' ;; \
    *) echo >&2 "error: unsupported architecture: ${apkArch} (see ${HASHICORP_RELEASES}/${NAME}/${VERSION}/)" && exit 1 ;; \
    esac && \
    wget ${HASHICORP_RELEASES}/${NAME}/${VERSION}/${NAME}_${VERSION}_linux_${ARCH}.zip && \
    wget ${HASHICORP_RELEASES}/${NAME}/${VERSION}/${NAME}_${VERSION}_SHA256SUMS && \
    wget ${HASHICORP_RELEASES}/${NAME}/${VERSION}/${NAME}_${VERSION}_SHA256SUMS.sig && \
    gpg --batch --verify ${NAME}_${VERSION}_SHA256SUMS.sig ${NAME}_${VERSION}_SHA256SUMS && \
    grep ${NAME}_${VERSION}_linux_${ARCH}.zip ${NAME}_${VERSION}_SHA256SUMS | sha256sum -c && \
    unzip -d /bin ${NAME}_${VERSION}_linux_${ARCH}.zip && \
    cd /tmp && \
    rm -rf /tmp/build && \
    apk del gnupg openssl && \
    rm -rf /root/.gnupg

USER 100
CMD /bin/${NAME}
