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

# hayduk stages browser file transfers here; the msf container bind-mounts
# the same path. Create it owned by the current user before docker does.
mkdir -p -m 700 "${HAYDUK_STAGING:-/tmp/hayduk-staging}"

# shellcheck disable=SC2086
docker compose $profile up -d
echo "waiting for msfrpcd..."
for i in $(seq 1 90); do
  # the daemon's "ready" line sits in buffered stdout; probe the port instead
  if docker compose exec -T msf ruby -rsocket -e 'TCPSocket.new("127.0.0.1", 55553).close' >/dev/null 2>&1; then
    echo "msfrpcd ready on 127.0.0.1:55553"
    if [ -n "$profile" ]; then
      for box in hayduk-vulnbox hayduk-sshbox; do
        ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$box" 2>/dev/null || true)
        [ -n "$ip" ] && echo "$box up at $ip"
      done
      msfip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' msf-msf-1 2>/dev/null || true)
      [ -n "$msfip" ] && echo "msf container at $msfip (callback host for target-container sessions)"
    fi
    exit 0
  fi
  sleep 2
done
echo "msfrpcd did not become ready" >&2
exit 1
