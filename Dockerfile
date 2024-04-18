# Build the manager binary
ARG REPO="hub.easystack.io/production"
ARG GOLANG_VERSION=x.x.x
FROM ${REPO}/escloud-linux-source-es8-base:6.2.1 as builder
ARG TARGETOS
ARG TARGETARCH

RUN dnf install -y wget make git gcc tar

ARG GOLANG_VERSION=0.0.0
RUN set -eux; \
    \
    arch="$(uname -m)"; \
    case "${arch##*-}" in \
        x86_64 | amd64) ARCH='amd64' ;; \
        ppc64el | ppc64le) ARCH='ppc64le' ;; \
        aarch64) ARCH='arm64' ;; \
        *) echo "unsupported architecture" ; exit 1 ;; \
    esac; \
    wget -nv -O - https://storage.googleapis.com/golang/go${GOLANG_VERSION}.linux-${ARCH}.tar.gz \
    | tar -C /usr/local -xz

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH
ENV GOPROXY http://goproxy.easystack.io,direct

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/config-manager/main.go cmd/config-manager/main.go
COPY api/ api/
COPY internal/controller/ internal/controller/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o config-manager cmd/config-manager/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM ${REPO}/escloud-linux-source-es8-base:6.2.1
ARG VERSION="unknown"
ARG GIT_COMMIT="unknown"
ENV VERSION ${VERSION}
ENV GIT_COMMIT ${GIT_COMMIT}

WORKDIR /
COPY --from=builder /workspace/config-manager .
USER 65532:65532

ENTRYPOINT ["/config-manager"]
