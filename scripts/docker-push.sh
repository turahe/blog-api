#!/usr/bin/env bash
# Build the production image for several platforms and push it to Docker Hub.
#
#   scripts/docker-push.sh                       # turahe/blog-api:<git describe> and :latest
#   scripts/docker-push.sh -t v1.4.0 --no-latest
#   DOCKERHUB_REPO=me/blog-api scripts/docker-push.sh --platform linux/amd64
#
# Auth: uses the existing `docker login`, or logs in non-interactively when DOCKERHUB_USERNAME
# and DOCKERHUB_TOKEN (a Docker Hub access token) are set.
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: scripts/docker-push.sh [options]

Options:
  -r, --repo REPO        Docker Hub repository (default: $DOCKERHUB_REPO or turahe/blog-api)
  -t, --tag TAG          Image tag (default: git describe --tags --always --dirty)
      --latest           Also tag :latest (default, skipped for -dirty tags)
      --no-latest        Do not tag :latest
  -p, --platform LIST    Comma-separated platforms (default: linux/amd64,linux/arm64)
      --allow-dirty      Push even when the working tree has uncommitted changes
  -n, --dry-run          Print the build command without running it
  -h, --help             Show this help

Environment:
  DOCKERHUB_REPO, DOCKERHUB_USERNAME, DOCKERHUB_TOKEN, BUILDX_BUILDER
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

cd "$(dirname "${BASH_SOURCE[0]}")/.."

repo="${DOCKERHUB_REPO:-turahe/blog-api}"
tag=""
latest="auto"
platforms="linux/amd64,linux/arm64"
allow_dirty=false
dry_run=false

while [[ $# -gt 0 ]]; do
	case "$1" in
	-r | --repo) repo="${2:?--repo needs a value}"; shift 2 ;;
	-t | --tag) tag="${2:?--tag needs a value}"; shift 2 ;;
	--latest) latest=true; shift ;;
	--no-latest) latest=false; shift ;;
	-p | --platform) platforms="${2:?--platform needs a value}"; shift 2 ;;
	--allow-dirty) allow_dirty=true; shift ;;
	-n | --dry-run) dry_run=true; shift ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; die "unknown option: $1" ;;
	esac
done

command -v docker >/dev/null || die "docker is not installed"
docker buildx version >/dev/null 2>&1 || die "docker buildx is required for multi-platform builds"

commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
dirty=false
if [[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]]; then
	dirty=true
fi

if [[ "$dirty" == true && "$allow_dirty" != true ]]; then
	die "working tree has uncommitted changes; commit them or pass --allow-dirty"
fi

if [[ -z "$tag" ]]; then
	tag="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi

[[ "$tag" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || die "invalid image tag: $tag"

if [[ "$latest" == auto ]]; then
	latest=true
	[[ "$tag" == *-dirty ]] && latest=false
fi

tags=(--tag "docker.io/${repo}:${tag}")
[[ "$latest" == true ]] && tags+=(--tag "docker.io/${repo}:latest")

cmd=(
	docker buildx build
	--target production
	--platform "$platforms"
	--build-arg "VERSION=${tag}"
	--build-arg "COMMIT=${commit}"
	--build-arg "BUILD_TIME=${build_time}"
	--label "org.opencontainers.image.source=https://github.com/turahe/blog-api"
	--label "org.opencontainers.image.revision=${commit}"
	--label "org.opencontainers.image.version=${tag}"
	--label "org.opencontainers.image.created=${build_time}"
	--provenance=mode=max
	--sbom=true
	"${tags[@]}"
	--push
	.
)

if [[ "$dry_run" == true ]]; then
	printf '%q ' "${cmd[@]}"
	echo
	exit 0
fi

if [[ -n "${DOCKERHUB_TOKEN:-}" ]]; then
	[[ -n "${DOCKERHUB_USERNAME:-}" ]] || die "DOCKERHUB_TOKEN is set but DOCKERHUB_USERNAME is not"
	printf '%s' "$DOCKERHUB_TOKEN" | docker login docker.io --username "$DOCKERHUB_USERNAME" --password-stdin
fi

echo "Pushing docker.io/${repo}:${tag}$([[ "$latest" == true ]] && echo " and :latest") for ${platforms}"
"${cmd[@]}"

echo "Pushed: https://hub.docker.com/r/${repo}/tags"
