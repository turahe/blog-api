# syntax=docker/dockerfile:1.7
# Build OpenAPI bundles with the official Redocly CLI image (no local Node required).
#
#   make contracts          # lint + bundle into contracts/
#   make contracts-docs     # also emit dist/openapi.html
#
FROM redocly/cli:2.54.2 AS tools
WORKDIR /spec

COPY redocly.yaml ./
COPY contracts/openapi.yaml ./contracts/openapi.yaml
COPY paths ./paths
COPY components ./components

RUN redocly lint contracts/openapi.yaml \
 && redocly bundle contracts/openapi.yaml -o contracts/openapi.bundle.yaml \
 && redocly bundle contracts/openapi.yaml --dereferenced -o contracts/openapi.bundle.deref.yaml

FROM tools AS docs
RUN mkdir -p dist \
 && redocly build-docs contracts/openapi.yaml -o dist/openapi.html

# Scratch export stages for `docker build --output`
FROM scratch AS bundle
COPY --from=tools /spec/contracts/openapi.bundle.yaml /openapi.bundle.yaml
COPY --from=tools /spec/contracts/openapi.bundle.deref.yaml /openapi.bundle.deref.yaml

FROM scratch AS html
COPY --from=docs /spec/dist/openapi.html /openapi.html
