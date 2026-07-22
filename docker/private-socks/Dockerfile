FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETARCH
ARG BUILD_VERSION=unknown
ARG BUILD_REVISION=unknown

LABEL org.opencontainers.image.title="sing-box private SOCKS HTTP proxy" \
      org.opencontainers.image.description="Multi-platform HTTP proxy backed by the custom sing-box private SOCKS outbound" \
      org.opencontainers.image.source="https://github.com/ztyawc/sing-box" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.revision="${BUILD_REVISION}"

COPY --chown=65532:65532 dist/sing-box-private-socks-linux-${TARGETARCH} /usr/local/bin/sing-box
COPY --chown=65532:65532 docker/private-socks/config.example.json /etc/sing-box/config.example.json

USER 65532:65532
EXPOSE 8080/tcp

ENTRYPOINT ["/usr/local/bin/sing-box"]
CMD ["run", "-c", "/etc/sing-box/config.json"]
