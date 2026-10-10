#!/usr/bin/env bash
# Prepares a checkout to run: creates .env, fills in missing secrets, and
# imports the Google OAuth client. Safe to run again; it never overwrites a value.
#
# Usage: scripts/setup.sh [path/to/client_secret.json]
set -euo pipefail
cd "$(dirname "$0")/.."

ok()   { printf '  \033[32mok\033[0m    %s\n' "$*"; }
warn() { printf '  \033[33mtodo\033[0m  %s\n' "$*"; }

echo "Checking tools"
have() { command -v "$1" >/dev/null 2>&1; }
if have podman; then ok "podman"; elif have docker; then ok "docker"; else warn "install podman or docker"; fi
have go && ok "$(go version | cut -d' ' -f3) (only needed for make test / run-api / run-sync)" || warn "go not found (only needed for local development)"
have openssl || { echo "openssl is required to generate secrets"; exit 1; }
if have podman && [ -n "${XDG_RUNTIME_DIR:-}" ] && [ ! -S "$XDG_RUNTIME_DIR/podman/podman.sock" ]; then
  warn "make test needs the Podman socket: systemctl --user enable --now podman.socket"
fi

echo "Configuring .env"
[ -f .env ] || { cp .env.example .env; ok "created .env from .env.example"; }

get() { grep -E "^$1=" .env | head -1 | cut -d= -f2- || true; }
put() {
  if grep -qE "^$1=" .env; then
    local tmp; tmp=$(mktemp); awk -v k="$1" -v v="$2" 'BEGIN{FS=OFS="="} $1==k{$0=k"="v} {print}' .env > "$tmp" && mv "$tmp" .env
  else
    printf '%s=%s\n' "$1" "$2" >> .env
  fi
}
fill() { # key, generator command
  if [ -n "$(get "$1")" ]; then ok "$1 already set"; else put "$1" "$(eval "$2")"; ok "$1 generated"; fi
}

fill LEAD_SIGNING_SECRET  "openssl rand -hex 32"
fill ADMIN_API_KEY        "openssl rand -hex 32"
fill TOKEN_ENCRYPTION_KEY "openssl rand -base64 32"

json=${1:-}
if [ -z "$json" ]; then
  shopt -s nullglob; found=(client_secret_*.json); shopt -u nullglob
  [ ${#found[@]} -eq 1 ] && json=${found[0]}
fi
if [ -n "$(get GOOGLE_CLIENT_ID)" ] && [ -n "$(get GOOGLE_CLIENT_SECRET)" ]; then
  ok "Google client already set"
elif [ -n "$json" ]; then
  have python3 || { warn "python3 is needed to read $json; copy client_id and client_secret into .env by hand"; json=; }
  if [ -n "$json" ]; then
    read -r id secret < <(python3 -I -c 'import json,sys; c=json.load(open(sys.argv[1])); c=c.get("web") or c.get("installed"); print(c["client_id"], c["client_secret"])' "$json")
    put GOOGLE_CLIENT_ID "$id"; put GOOGLE_CLIENT_SECRET "$secret"
    ok "Google client imported from $json"
  fi
else
  warn "Google client not set: download the OAuth client JSON into this folder and run make setup again"
fi

port=$(get PORT); port=${port:-8080}
cat <<NEXT

Next steps
  make up                     start db, lead-api and sheets-sync
  open http://localhost:$port/auth/google?key=$(get ADMIN_API_KEY)
                              link the Google account that should own the sheet
  scripts/send-lead.sh '{"id":"p-1","name":"Ada","email":"ada@example.com","company":"Acme","source":"partner"}'
NEXT
