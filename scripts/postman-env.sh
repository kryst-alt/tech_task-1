#!/usr/bin/env bash
# Writes postman/local.postman_environment.json with the secrets from .env filled in.
# The file is git-ignored; import it into Postman next to the collection.
set -euo pipefail
cd "$(dirname "$0")/.."
get() { grep "^$1=" .env | cut -d= -f2-; }
SIGNING_SECRET=$(get LEAD_SIGNING_SECRET) ADMIN_API_KEY=$(get ADMIN_API_KEY) PORT=$(get PORT) python3 -I - <<'PY'
import json, os
env = json.load(open("postman/lead-api.postman_environment.json"))
env["name"] = "Lead API (local, with secrets)"
values = {"baseUrl": f"http://localhost:{os.environ['PORT'] or 8080}",
          "signingSecret": os.environ["SIGNING_SECRET"], "adminApiKey": os.environ["ADMIN_API_KEY"]}
for v in env["values"]:
    v["value"] = values[v["key"]]
json.dump(env, open("postman/local.postman_environment.json", "w"), indent=2)
PY
echo "wrote postman/local.postman_environment.json (git-ignored, holds secrets)"
