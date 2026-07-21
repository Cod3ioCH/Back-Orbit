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
  03-paperless-ngx)
    generate_secret postgres_password
    generate_secret paperless_secret_key
    ;;
  04-immich)
    generate_secret database_password
    database_password=$(tr -d '\n' < "$target_dir/secrets/database_password")
    umask 077
    {
      echo "DB_PASSWORD=$database_password"
      echo "DB_USERNAME=postgres"
      echo "DB_DATABASE_NAME=immich"
      echo "IMMICH_VERSION=v2.7.5"
    } > "$target_dir/.env"
    ;;
  06-vaultwarden-postgresql)
    generate_secret postgres_password
    database_password=$(tr -d '\n' < "$target_dir/secrets/postgres_password")
    umask 077
    {
      echo "POSTGRES_PASSWORD=$database_password"
      echo "DATABASE_URL=postgresql://vaultwarden:$database_password@database:5432/vaultwarden"
    } > "$target_dir/.env"
    ;;
  07-gitea-postgresql)
    generate_secret postgres_password
    ;;
esac

echo "$target_dir"
