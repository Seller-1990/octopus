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

assert_metadata "octopus-linux-x86_64" "octopus-linux-x86_64.zip" "octopus"
assert_metadata "octopus-windows-x86_64.exe" "octopus-windows-x86_64.zip" "octopus.exe"
assert_metadata "octopus-desktop-x86_64.exe" "octopus-desktop-x86_64.zip" "octopus-desktop.exe"

fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/octopus-archive-test.XXXXXX")"
trap 'rm -rf "${fixture_dir}"' EXIT
mkdir -p "${fixture_dir}/build/bin" "${fixture_dir}/build/archives"
printf 'fixture' > "${fixture_dir}/README.md"
printf 'fixture' > "${fixture_dir}/LICENSE"
touch \
    "${fixture_dir}/build/bin/octopus-linux-x86_64" \
    "${fixture_dir}/build/bin/octopus-windows-x86_64.exe" \
    "${fixture_dir}/build/bin/octopus-desktop-x86_64.exe"

(
    cd "${fixture_dir}"
    create_archives >/dev/null
)

for archive in \
    "octopus-linux-x86_64.zip" \
    "octopus-windows-x86_64.zip" \
    "octopus-desktop-x86_64.zip"; do
    test -f "${fixture_dir}/build/archives/${archive}"
done

linux_entries="$(unzip -Z1 "${fixture_dir}/build/archives/octopus-linux-x86_64.zip")"
windows_entries="$(unzip -Z1 "${fixture_dir}/build/archives/octopus-windows-x86_64.zip")"
desktop_entries="$(unzip -Z1 "${fixture_dir}/build/archives/octopus-desktop-x86_64.zip")"
grep -Fxq "octopus" <<<"${linux_entries}"
grep -Fxq "octopus.exe" <<<"${windows_entries}"
grep -Fxq "octopus-desktop.exe" <<<"${desktop_entries}"
printf 'archive naming and contents: ok\n'
