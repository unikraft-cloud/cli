# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
ARG TARGETOS TARGETARCH
RUN --mount=type=bind,target=. \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/unikraft \
      -ldflags "-s -w \
        -X unikraft.com/x/version.Name=unikraft-cli \
        -X unikraft.com/x/version.Docs=https://unikraft.com/docs/cli \
        -X unikraft.com/x/version.Issues=https://github.com/unikraft-cloud/cli/issues" \
      ./cmd/unikraft

FROM alpine:3.24
COPY --from=build /out/unikraft /usr/local/bin/unikraft
ENTRYPOINT ["unikraft"]
