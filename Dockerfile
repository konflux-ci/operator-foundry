FROM registry.access.redhat.com/ubi9/go-toolset:9.8-1788409979 AS builder

WORKDIR /opt/app-root/src

# Copy all sources, including go.mod and go.sum, at once
COPY --chown=1001:0 . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o operator-foundry ./cmd/operator-foundry

## Final image

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:5b74fce9d6e629942a0c6dc0f546c193e70d7f974d999a48c948c53dd3d36362

ARG PATH_TO_ART=/cachi2/output/deps/generic

LABEL \
  name="operator-foundry" \
  com.redhat.component="konflux-operator-foundry" \
  description="CLI for Konflux operator pipeline tasks" \
  io.k8s.description="CLI for Konflux operator pipeline tasks" \
  io.k8s.display-name="operator-foundry" \
  summary="Konflux operator pipeline task CLI" \
  io.openshift.tags="konflux,operator,olm,fbc"

RUN set -eux; \
    install_opm() { \
      version="$1"; \
      cp "${PATH_TO_ART}/linux-amd64-opm-${version}" "/usr/local/bin/opm-${version}"; \
      chmod 0555 "/usr/local/bin/opm-${version}"; \
    }; \
    install_opm "v1.26.4"; \
    install_opm "v1.28.0"; \
    install_opm "v1.40.0"; \
    install_opm "v1.44.0"; \
    install_opm "v1.48.0"; \
    install_opm "v1.50.0"; \
    install_opm "v1.57.0"; \
    install_opm "v1.61.0"; \
    install_opm "v1.67.0"; \
    install_opm "v1.69.0"; \
    install_opm "v1.73.0"

COPY --from=builder /opt/app-root/src/operator-foundry /usr/local/bin/operator-foundry
COPY LICENSE /licenses/LICENSE

# OpenShift preflight and Tekton task compatibility
USER 1001

ENTRYPOINT ["/usr/local/bin/operator-foundry"]
