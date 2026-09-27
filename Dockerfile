# syntax=docker/dockerfile:1.7

# Targets:
#   dev         live-reload toolchain image; expects the repo bind-mounted at /src (compose.dev.yaml)
#   production  static binary on distroless (default: `docker build .` builds this)

ARG GO_VERSION=1.27

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS deps
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download

FROM deps AS dev
ARG AIR_VERSION=v1.67.4
RUN go install github.com/air-verse/air@${AIR_VERSION}
# The bind-mounted repo has .git but the image has no git binary.
ENV GOFLAGS=-buildvcs=false
EXPOSE 8080 9090
CMD ["air", "-c", ".air.toml", "--", "serve"]

FROM deps AS build
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X github.com/turahe/blog-api/cmd.version=${VERSION} -X github.com/turahe/blog-api/cmd.commit=${COMMIT} -X github.com/turahe/blog-api/cmd.buildTime=${BUILD_TIME}" \
    -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot AS production
COPY --from=build /out/app /bin/app
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/bin/app"]
CMD ["serve"]
