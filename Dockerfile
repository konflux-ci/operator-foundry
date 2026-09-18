FROM registry.access.redhat.com/ubi9/go-toolset:9.8-1789040808 AS builder

WORKDIR /opt/app-root/src

# Copy all sources, including go.mod and go.sum, at once
COPY --chown=1001:0 . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o operator-foundry ./cmd/operator-foundry

## Final image

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:7b8e25a1b56ca4d00219198f3b5b51a3e1693a5c4f5369c5e190d7d6cb3f980e

LABEL \
  name="operator-foundry" \
  com.redhat.component="konflux-operator-foundry" \
  description="CLI for Konflux operator pipeline tasks" \
  io.k8s.description="CLI for Konflux operator pipeline tasks" \
  io.k8s.display-name="operator-foundry" \
  summary="Konflux operator pipeline task CLI" \
  io.openshift.tags="konflux,operator,olm,fbc"

COPY --from=builder /opt/app-root/src/operator-foundry /usr/local/bin/operator-foundry
COPY LICENSE /licenses/LICENSE

# OpenShift preflight and Tekton task compatibility
USER 1001

ENTRYPOINT ["/usr/local/bin/operator-foundry"]
