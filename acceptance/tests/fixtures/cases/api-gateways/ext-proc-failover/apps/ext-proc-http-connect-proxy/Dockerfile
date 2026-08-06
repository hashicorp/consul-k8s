FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY main.go ./
RUN go build -o /out/ext-proc-http-connect-proxy ./main.go

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/ext-proc-http-connect-proxy /bin/ext-proc-http-connect-proxy
EXPOSE 9000
ENTRYPOINT ["/bin/ext-proc-http-connect-proxy"]
