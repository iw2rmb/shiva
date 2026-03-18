# syntax=docker/dockerfile:1.7

ARG ALPINE_VERSION=3.21

FROM alpine:${ALPINE_VERSION} AS certs
RUN apk add --no-cache ca-certificates

FROM scratch

ARG TARGETARCH
COPY bin/shivad-linux-${TARGETARCH} /shivad
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/shivad"]
