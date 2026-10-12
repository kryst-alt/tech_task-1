#!/usr/bin/env bash
# Writes postman/local.postman_environment.json with the secrets from .env filled in.
# The file is git-ignored; import it into Postman next to the collection.
# Usage: scripts/postman-env.sh [base-url]   (default http://localhost:$PORT)
set -euo pipefail
cd "$(dirname "$0")/.."
get() { grep "^$1=" .env | cut -d= -f2-; }
base_url=${1:-http://localhost:$(get PORT | grep . || echo 8080)}
BASE_URL=$base_url SIGNING_SECRET=$(get LEAD_SIGNING_SECRET) ADMIN_API_KEY=$(get ADMIN_API_KEY) python3 -I - <<'PY'
import json, os
env = json.load(open("postman/lead-api.postman_environment.json"))
env["name"] = f"Lead API ({os.environ['BASE_URL'].rstrip('/')})"
values = {"baseUrl": os.environ["BASE_URL"].rstrip("/"),
          "signingSecret": os.environ["SIGNING_SECRET"], "adminApiKey": os.environ["ADMIN_API_KEY"]}
for v in env["values"]:
    v["value"] = values[v["key"]]
json.dump(env, open("postman/local.postman_environment.json", "w"), indent=2)
PY
echo "wrote postman/local.postman_environment.json for $base_url (git-ignored, holds secrets)"
