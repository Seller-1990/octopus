#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/build.sh"

assert_metadata() {
    local binary_name="$1"
    local expected_archive="$2"
    local expected_entry="$3"
    local archive_file entry

    IFS=$'\t' read -r archive_file entry < <(archive_metadata "${binary_name}")
    [[ "${archive_file}" == "${expected_archive}" ]] || {
        printf 'archive name for %s: got %s, want %s\n' "${binary_name}" "${archive_file}" "${expected_archive}" >&2
        exit 1
    }
    [[ "${entry}" == "${expected_entry}" ]] || {
        printf 'archive entry for %s: got %s, want %s\n' "${binary_name}" "${entry}" "${expected_entry}" >&2
        exit 1
    }
}

# archive_metadata still maps correctly for all platforms
assert_metadata "octopus-linux-x86_64" "octopus-linux-x86_64.zip" "octopus"
assert_metadata "octopus-windows-x86_64.exe" "octopus-windows-x86_64.zip" "octopus.exe"
assert_metadata "octopus-desktop-x86_64.exe" "octopus-desktop-x86_64.zip" "octopus-desktop.exe"
assert_metadata "octopus-darwin-arm64" "octopus-darwin-arm64.zip" "octopus"

# create_archives now only zips macOS binaries; Linux/Windows are skipped
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/octopus-archive-test.XXXXXX")"
trap 'rm -rf "${fixture_dir}"' EXIT
mkdir -p "${fixture_dir}/build/bin" "${fixture_dir}/build/archives"
printf 'fixture' > "${fixture_dir}/README.md"
printf 'fixture' > "${fixture_dir}/LICENSE"
touch \
    "${fixture_dir}/build/bin/octopus-linux-x86_64" \
    "${fixture_dir}/build/bin/octopus-windows-x86_64.exe" \
    "${fixture_dir}/build/bin/octopus-desktop-x86_64.exe" \
    "${fixture_dir}/build/bin/octopus-darwin-arm64" \
    "${fixture_dir}/build/bin/octopus-darwin-x86_64"

(
    cd "${fixture_dir}"
    create_archives >/dev/null
)

# Only macOS archives should be created
for archive in \
    "octopus-darwin-arm64.zip" \
    "octopus-darwin-x86_64.zip"; do
    test -f "${fixture_dir}/build/archives/${archive}" || {
        printf '❌ Expected archive missing: %s\n' "${archive}" >&2
        exit 1
    }
done

# Linux/Windows zips should NOT exist
for archive in \
    "octopus-linux-x86_64.zip" \
    "octopus-windows-x86_64.zip" \
    "octopus-desktop-x86_64.zip"; do
    test ! -f "${fixture_dir}/build/archives/${archive}" || {
        printf '❌ Unexpected archive found (should be skipped): %s\n' "${archive}" >&2
        exit 1
    }
done

darwin_entries="$(unzip -Z1 "${fixture_dir}/build/archives/octopus-darwin-arm64.zip")"
grep -Fxq "octopus" <<<"${darwin_entries}"
printf 'archive naming and contents: ok\n'
