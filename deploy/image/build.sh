#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

image_repo="${IMAGE_REPO:-ghcr.io/iw2rmb/shiva}"
image_tag="${IMAGE_TAG:-$(git -C "${repo_root}" rev-parse --short=12 HEAD)}"
platforms="${PLATFORMS:-linux/amd64,linux/arm64}"
builder_name="${BUILDER_NAME:-shiva-multiarch}"
push_latest="${PUSH_LATEST:-1}"

usage() {
	cat <<'EOF'
Build and push Shiva image to GHCR (multi-arch).

Environment:
  IMAGE_REPO    Target image repo (default: ghcr.io/iw2rmb/shiva)
  IMAGE_TAG     Target image tag (default: current git short SHA)
  PLATFORMS     Buildx platform list (default: linux/amd64,linux/arm64)
  BUILDER_NAME  Buildx builder name (default: shiva-multiarch)
  PUSH_LATEST   Also push :latest tag when set to 1 (default: 1)
  GHCR_USERNAME Optional GHCR username for docker login
  GHCR_TOKEN    Optional GHCR token for docker login

Examples:
  IMAGE_TAG=v1.2.3 ./deploy/image/build.sh
  IMAGE_REPO=ghcr.io/acme/shiva IMAGE_TAG=main ./deploy/image/build.sh
EOF
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	usage
	exit 0
fi

if [[ -n "${GHCR_TOKEN:-}" ]]; then
	if [[ -z "${GHCR_USERNAME:-}" ]]; then
		echo "GHCR_USERNAME must be set when GHCR_TOKEN is provided" >&2
		exit 1
	fi
	printf '%s' "${GHCR_TOKEN}" | docker login ghcr.io --username "${GHCR_USERNAME}" --password-stdin
fi

if ! docker buildx inspect "${builder_name}" >/dev/null 2>&1; then
	docker buildx create --name "${builder_name}" --driver docker-container --use >/dev/null
else
	docker buildx use "${builder_name}" >/dev/null
fi
docker buildx inspect --bootstrap >/dev/null

tags=(-t "${image_repo}:${image_tag}")
if [[ "${push_latest}" == "1" ]]; then
	tags+=(-t "${image_repo}:latest")
fi

docker buildx build \
	--file "${script_dir}/Dockerfile" \
	--platform "${platforms}" \
	"${tags[@]}" \
	--push \
	"${repo_root}"

echo "Pushed image:"
echo "  - ${image_repo}:${image_tag}"
if [[ "${push_latest}" == "1" ]]; then
	echo "  - ${image_repo}:latest"
fi
