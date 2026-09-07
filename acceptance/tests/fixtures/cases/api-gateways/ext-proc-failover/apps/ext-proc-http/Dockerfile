# Copyright IBM Corp. 2018, 2026
# SPDX-License-Identifier: MPL-2.0

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY main.go ./
RUN go build -o /out/ext-proc-http ./main.go

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/ext-proc-http /bin/ext-proc-http
EXPOSE 9000
ENTRYPOINT ["/bin/ext-proc-http"]
