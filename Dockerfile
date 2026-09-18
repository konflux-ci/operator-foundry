# opm binary source stages (one per supported OCP minor version)
# v4.12–v4.13: RHEL 8 image (no -rhel9 variant available)
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.12      AS opm-4-12
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.13      AS opm-4-13
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.14      AS opm-4-14
# v4.15+: RHEL 9 image
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.15 AS opm-4-15
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.16 AS opm-4-16
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.17 AS opm-4-17
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.18 AS opm-4-18
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.19 AS opm-4-19
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.20 AS opm-4-20
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.21 AS opm-4-21
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.22 AS opm-4-22

FROM registry.access.redhat.com/ubi9/go-toolset:9.8-1789040808 AS builder

WORKDIR /opt/app-root/src

# Copy all sources, including go.mod and go.sum, at once
COPY --chown=1001:0 . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o operator-foundry ./cmd/operator-foundry

## Final image

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:d235f607e1d6d833f031db107dc42206e4dd4d5aa9142c43d3771fb7f9bea76a

LABEL \
  name="operator-foundry" \
  com.redhat.component="konflux-operator-foundry" \
  description="CLI for Konflux operator pipeline tasks" \
  io.k8s.description="CLI for Konflux operator pipeline tasks" \
  io.k8s.display-name="operator-foundry" \
  summary="Konflux operator pipeline task CLI" \
  io.openshift.tags="konflux,operator,olm,fbc"

COPY --from=opm-4-12 /usr/bin/opm /usr/local/bin/opm-v4.12
COPY --from=opm-4-13 /usr/bin/opm /usr/local/bin/opm-v4.13
COPY --from=opm-4-14 /usr/bin/opm /usr/local/bin/opm-v4.14
COPY --from=opm-4-15 /usr/bin/opm /usr/local/bin/opm-v4.15
COPY --from=opm-4-16 /usr/bin/opm /usr/local/bin/opm-v4.16
COPY --from=opm-4-17 /usr/bin/opm /usr/local/bin/opm-v4.17
COPY --from=opm-4-18 /usr/bin/opm /usr/local/bin/opm-v4.18
COPY --from=opm-4-19 /usr/bin/opm /usr/local/bin/opm-v4.19
COPY --from=opm-4-20 /usr/bin/opm /usr/local/bin/opm-v4.20
COPY --from=opm-4-21 /usr/bin/opm /usr/local/bin/opm-v4.21
COPY --from=opm-4-22 /usr/bin/opm /usr/local/bin/opm-v4.22

COPY --from=builder /opt/app-root/src/operator-foundry /usr/local/bin/operator-foundry
COPY LICENSE /licenses/LICENSE

# OpenShift preflight and Tekton task compatibility
USER 1001

ENTRYPOINT ["/usr/local/bin/operator-foundry"]
