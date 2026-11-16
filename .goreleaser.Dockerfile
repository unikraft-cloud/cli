FROM alpine:3.24
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/unikraft /usr/local/bin/unikraft
ENTRYPOINT ["unikraft"]
