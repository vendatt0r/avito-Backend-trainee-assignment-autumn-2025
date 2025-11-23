#!/bin/sh
set -e

host="$1"
shift

until nc -z "$host" 5432; do
  echo "Waiting for postgres at $host..."
  sleep 1
done

exec "$@"
