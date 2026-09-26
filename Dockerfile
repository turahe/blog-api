# syntax=docker/dockerfile:1.7
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/turahe/blog-api/cmd.version=${VERSION} -X github.com/turahe/blog-api/cmd.commit=${COMMIT} -X github.com/turahe/blog-api/cmd.buildTime=${BUILD_TIME}" \
    -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /bin/app
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/bin/app"]
CMD ["serve"]
