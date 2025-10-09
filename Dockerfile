# syntax=docker/dockerfile:1
# Build the manager binary
FROM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Configure git to use SSH for private GitHub repos
RUN --mount=type=ssh \
    mkdir -p ~/.ssh && \
    ssh-keyscan github.com >> ~/.ssh/known_hosts && \
    git config --global url."git@github.com:".insteadOf "https://github.com/"

# Set GOPRIVATE to tell Go which repos are private
ENV GOPRIVATE=github.com/rinswind/*

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=ssh go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build
# the GOARCH has no default value to allow the binary to be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
