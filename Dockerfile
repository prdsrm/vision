# syntax=docker/dockerfile:1
ARG GO_VERSION=1.22
FROM golang:${GO_VERSION}-alpine AS base
WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod/ \
    go mod download -x

FROM base AS build
ARG GOARCH=amd64
ENV CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH}
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod/ \
    go build -ldflags "-s -w" -o /advertiser

FROM alpine:latest AS prod
RUN apk update && apk add ca-certificates
RUN update-ca-certificates
COPY --from=build /advertiser /advertiser
ENTRYPOINT ["/advertiser"]

FROM scratch AS binaries
COPY --from=build /advertiser /advertiser
