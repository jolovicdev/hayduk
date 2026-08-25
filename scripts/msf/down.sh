#!/usr/bin/env sh
# Tear the whole lab down, vulnbox profile included, volumes with it.
cd "$(dirname "$0")"
docker compose --profile vulnbox down -v
