FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN go build -o /out/service-e ./main.go

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/service-e /bin/service-e
EXPOSE 8080
ENTRYPOINT ["/bin/service-e"]
