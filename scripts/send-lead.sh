#!/usr/bin/env bash
# Sign a lead the way the partner does and POST it to the local server.
# Usage: scripts/send-lead.sh '<json>' [secret]   (secret defaults to LEAD_SIGNING_SECRET from .env)
set -euo pipefail
cd "$(dirname "$0")/.."
body=${1:?usage: scripts/send-lead.sh '<json>' [secret]}
secret=${2:-$(grep '^LEAD_SIGNING_SECRET=' .env | cut -d= -f2-)}
ts=$(date +%s)
sig=$(printf '%s' "$ts.$body" | openssl dgst -sha256 -hmac "$secret" -hex | awk '{print $NF}')
curl -s -w '\nHTTP %{http_code}\n' -X POST "http://localhost:${PORT:-8080}/v1/leads" \
  -H 'Content-Type: application/json' -H "X-Timestamp: $ts" -H "X-Signature: sha256=$sig" -d "$body"
