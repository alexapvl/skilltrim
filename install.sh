#!/bin/sh

set -eu

repository="alexapvl/skilltrim"
version="${SKILLTRIM_VERSION:-latest}"
install_dir="${SKILLTRIM_INSTALL_DIR:-${HOME}/.local/bin}"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "error: unsupported operating system: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *)
    echo "error: unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required" >&2
  exit 1
fi

if [ "$version" = "latest" ]; then
  release_url="https://github.com/${repository}/releases/latest/download"
else
  case "$version" in
    v*) tag="$version" ;;
    *) tag="v${version}" ;;
  esac
  release_url="https://github.com/${repository}/releases/download/${tag}"
fi

archive="skilltrim_${os}_${arch}.tar.gz"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/skilltrim.XXXXXX")"
trap 'rm -rf -- "$tmp_dir"' EXIT HUP INT TERM

download() {
  curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error "$1" --output "$2"
}

download "${release_url}/${archive}" "${tmp_dir}/${archive}"
download "${release_url}/checksums.txt" "${tmp_dir}/checksums.txt"

expected="$(awk -v archive="$archive" '$2 == archive || $2 == "*" archive { print $1 }' "${tmp_dir}/checksums.txt")"
if [ -z "$expected" ]; then
  echo "error: checksum missing for ${archive}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${tmp_dir}/${archive}" | awk '{ print $1 }')"
else
  actual="$(shasum -a 256 "${tmp_dir}/${archive}" | awk '{ print $1 }')"
fi

if [ "$actual" != "$expected" ]; then
  echo "error: checksum verification failed for ${archive}" >&2
  exit 1
fi

tar -xzf "${tmp_dir}/${archive}" -C "$tmp_dir" skilltrim
mkdir -p "$install_dir"
install -m 0755 "${tmp_dir}/skilltrim" "${install_dir}/.skilltrim.new"
mv -f "${install_dir}/.skilltrim.new" "${install_dir}/skilltrim"

installed_version="$("${install_dir}/skilltrim" --version)"
echo "installed skilltrim ${installed_version} to ${install_dir}/skilltrim"

case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *) echo "add ${install_dir} to PATH to run skilltrim" ;;
esac
