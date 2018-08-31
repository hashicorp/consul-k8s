FROM golang:1.11.0 as builder
ARG GIT_COMMIT
ARG GIT_DIRTY
ARG GIT_DESCRIBE
ENV CONSUL_DEV=1
ENV COLORIZE=0
Add . /opt/build
WORKDIR /opt/build
RUN make

FROM consul:latest

COPY --from=builder /opt/build/bin/consul-k8s /bin
