FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
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

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
COPY --from=build /out/unikraft /usr/local/bin/unikraft
ENTRYPOINT ["unikraft"]
