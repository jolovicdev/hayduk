#!/usr/bin/env sh
# Start a disposable lab on 127.0.0.1:55553 (user msf, pass testpass123):
# msfrpcd with its database, plus --with-vulnbox for a disposable target
# machine stacked with old vulnerable services.
set -e
cd "$(dirname "$0")"

profile=""
if [ "${1:-}" = "--with-vulnbox" ]; then
  profile="--profile vulnbox"
fi

# shellcheck disable=SC2086
docker compose $profile up -d
echo "waiting for msfrpcd..."
for i in $(seq 1 90); do
  if docker compose logs msf 2>&1 | grep -qiE "MSGRPC ready|MSFRPC ready"; then
    echo "msfrpcd ready on 127.0.0.1:55553"
    if [ -n "$profile" ]; then
      for box in hayduk-vulnbox hayduk-sshbox; do
        ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$box" 2>/dev/null || true)
        [ -n "$ip" ] && echo "$box up at $ip"
      done
    fi
    exit 0
  fi
  sleep 2
done
echo "msfrpcd did not become ready" >&2
exit 1
