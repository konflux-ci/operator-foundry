# -------------------------------------------------------------------
# opm source stages
# -------------------------------------------------------------------
# Each OCP ose-operator-registry image ships the opm binary that
# matches that OCP release at /usr/bin/opm.  The version mapping below
# is approximate except where marked "(confirmed)".  Verify with:
#
#   podman run --rm --entrypoint="" <image> opm version
#
#   opm ~v1.26 <- OCP 4.7       opm ~v1.50 <- OCP 4.13
#   opm ~v1.28 <- OCP 4.8       opm ~v1.57 <- OCP 4.14
#   opm ~v1.40 <- OCP 4.10      opm ~v1.61 <- OCP 4.15
#   opm ~v1.44 <- OCP 4.11      opm ~v1.67 <- OCP 4.18 (confirmed)
#   opm ~v1.48 <- OCP 4.12      opm ~v1.69 <- OCP 4.19
#                                opm ~v1.73 <- OCP 4.20
# -------------------------------------------------------------------

FROM registry.redhat.io/openshift4/ose-operator-registry:v4.7        AS opm-v1-26
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.8        AS opm-v1-28
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.10       AS opm-v1-40
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.11       AS opm-v1-44
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.12       AS opm-v1-48
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.13       AS opm-v1-50
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.14       AS opm-v1-57
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.15 AS opm-v1-61
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.18 AS opm-v1-67
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.19 AS opm-v1-69
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.20 AS opm-v1-73

# -------------------------------------------------------------------
# Go builder
# -------------------------------------------------------------------

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

# opm binaries from Red Hat operator-registry images (EC-compliant source)
COPY --from=opm-v1-26 /usr/bin/opm /opm/v1.26.0/opm
COPY --from=opm-v1-28 /usr/bin/opm /opm/v1.28.0/opm
COPY --from=opm-v1-40 /usr/bin/opm /opm/v1.40.0/opm
COPY --from=opm-v1-44 /usr/bin/opm /opm/v1.44.0/opm
COPY --from=opm-v1-48 /usr/bin/opm /opm/v1.48.0/opm
COPY --from=opm-v1-50 /usr/bin/opm /opm/v1.50.0/opm
COPY --from=opm-v1-57 /usr/bin/opm /opm/v1.57.0/opm
COPY --from=opm-v1-61 /usr/bin/opm /opm/v1.61.0/opm
COPY --from=opm-v1-67 /usr/bin/opm /opm/v1.67.0/opm
COPY --from=opm-v1-69 /usr/bin/opm /opm/v1.69.0/opm
COPY --from=opm-v1-73 /usr/bin/opm /opm/v1.73.0/opm

COPY --from=builder /opt/app-root/src/operator-foundry /usr/local/bin/operator-foundry
COPY LICENSE /licenses/LICENSE

# OpenShift preflight and Tekton task compatibility
USER 1001

ENTRYPOINT ["/usr/local/bin/operator-foundry"]
