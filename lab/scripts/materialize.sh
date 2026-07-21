#!/bin/sh
set -eu

scenario="${1:-}"
case "$scenario" in
  ""|*[!a-z0-9-]*)
    echo "usage: $0 <scenario-id>" >&2
    exit 2
    ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
lab_dir=$(dirname -- "$script_dir")
source_dir="$lab_dir/templates/$scenario"
target_dir="$lab_dir/runtime/projects/$scenario"

if [ ! -d "$source_dir" ]; then
  echo "unknown or not-yet-ready scenario: $scenario" >&2
  exit 1
fi
if [ -e "$target_dir" ]; then
  echo "target already exists; refusing to overwrite state: $target_dir" >&2
  exit 1
fi

mkdir -p "$(dirname -- "$target_dir")"
cp -R "$source_dir" "$target_dir"
mkdir -m 0700 "$target_dir/secrets"

generate_secret() {
  name="$1"
  umask 077
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24 > "$target_dir/secrets/$name"
  else
    echo "openssl is required to generate lab credentials" >&2
    exit 1
  fi
}

case "$scenario" in
  01-wordpress-mariadb)
    generate_secret mariadb_root_password
    generate_secret mariadb_password
    ;;
  02-nextcloud-postgresql)
    generate_secret postgres_password
    generate_secret nextcloud_admin_password
    ;;
esac

echo "$target_dir"
