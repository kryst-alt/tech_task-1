#!/usr/bin/env bash
# Runs the Go tests and the acceptance tests with a readable pass/fail report.
#
#   scripts/test.sh                       everything
#   scripts/test.sh go | acceptance       one suite
#   scripts/test.sh signature             one area, both suites
#   scripts/test.sh acceptance stages     one area, one suite
#
# Areas: signature duplicates email-update stages sync health config
# The acceptance tests need the stack running (make up).
set -uo pipefail
cd "$(dirname "$0")/.."

suite=${1:-all}
area=${2:-}
case "$suite" in
  all | go | acceptance) ;;
  *) area=$suite; suite=all ;;
esac

# Go packages and test-name filter for each area.
go_pkgs=./... go_run=.
case "$area" in
  "") ;;
  signature)    go_pkgs="./services/lead-api/api" go_run="InvalidSignature|Gzipped" ;;
  duplicates)   go_pkgs="./services/lead-api/api ./internal/store ./internal/lead" go_run="Duplicate|Race|^TestDecide$" ;;
  email-update) go_pkgs="./services/lead-api/api ./internal/lead" go_run="ExistingEmail|DifferentLeads|IgnoresEmailCase" ;;
  stages)       go_pkgs="./services/lead-api/api ./internal/lead" go_run="Stage|CanMoveTo" ;;
  sync)         go_pkgs="./internal/sheets ./services/lead-api/api" go_run="Written|Sync|Backoff|Revoked|NothingHappens|Linking|SlowSheet|Seal|RowNumber|GoogleSignIn|GoogleCallback" ;;
  health)       go_pkgs="./services/lead-api/api" go_run="Health" ;;
  config)       go_pkgs="./internal/config" ;;
  *) echo "unknown area: $area (signature duplicates email-update stages sync health config)"; exit 2 ;;
esac

if [ -t 1 ]; then bold=$'\e[1m' green=$'\e[32m' red=$'\e[31m' dim=$'\e[2m' reset=$'\e[0m'; else bold= green= red= dim= reset=; fi
banner() { printf '\n%s━━ %s %s%s\n' "$bold" "$1" "$(printf '━%.0s' $(seq 1 $((60 - ${#1}))))" "$reset"; }

# Without Docker, testcontainers reaches Podman through its socket (same rule as the Makefile).
if ! command -v docker >/dev/null && [ -S "${XDG_RUNTIME_DIR:-}/podman/podman.sock" ]; then
  export DOCKER_HOST="unix://$XDG_RUNTIME_DIR/podman/podman.sock" TESTCONTAINERS_RYUK_DISABLED=true
fi

go_result= acceptance_result=

if [ "$suite" != acceptance ]; then
  banner "Go tests${area:+: $area}"
  # shellcheck disable=SC2086
  if go tool gotestsum --format testdox --hide-summary=skipped -- -count=1 -run "$go_run" $go_pkgs; then
    go_result=PASS
  else
    go_result=FAIL
  fi
fi

if [ "$suite" != go ]; then
  banner "Acceptance tests (running system)${area:+: $area}"
  target=.
  [ -n "$area" ] && target="test_${area//-/_}.py"
  if [ ! -e "tests/acceptance/$target" ]; then
    echo "${dim}No acceptance tests for $area.${reset}"
  else
    venv=tests/acceptance/.venv
    if [ ! -x "$venv/bin/pytest" ] || [ tests/acceptance/requirements.txt -nt "$venv/.installed" ]; then
      echo "${dim}Installing test dependencies...${reset}"
      python3 -m venv "$venv" && "$venv/bin/pip" install -q -r tests/acceptance/requirements.txt && touch "$venv/.installed"
    fi
    set -a; . ./.env; set +a
    if (cd tests/acceptance && "../../$venv/bin/pytest" "$target"); then
      acceptance_result=PASS
    else
      acceptance_result=FAIL
    fi
  fi
fi

banner Summary
status=0
for row in "Go tests:$go_result" "Acceptance tests:$acceptance_result"; do
  name=${row%%:*} result=${row#*:}
  [ -z "$result" ] && continue
  color=$green; [ "$result" = FAIL ] && { color=$red; status=1; }
  printf '  %-20s %s%s%s\n' "$name" "$bold$color" "$result" "$reset"
done
exit $status
