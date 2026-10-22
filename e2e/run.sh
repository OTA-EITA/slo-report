#!/usr/bin/env bash
# End-to-end test: import synthetic traffic into a real VictoriaMetrics and
# check the error budget report produced by slo-report.
set -euo pipefail

vm=${VM_URL:-http://localhost:8428}
bin=${BIN:-./bin/slo-report}

python3 e2e/gen_series.py | curl -sSf --data-binary @- "$vm/api/v1/import/prometheus"
curl -sSf "$vm/internal/force_flush" >/dev/null
# Freshly imported data becomes searchable after a short delay.
for _ in $(seq 30); do
  n=$(curl -sSf "$vm/api/v1/query" --data-urlencode 'query=count(http_requests_total)' | jq -r '.data.result[0].value[1] // "0"')
  [ "$n" = "4" ] && break
  sleep 2
done

$bin report --prometheus "$vm" --output json --fail-on never e2e/slos.yaml > report.json
$bin report --prometheus "$vm" --output markdown --fail-on never e2e/slos.yaml | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"

fail() { echo "e2e: $*" >&2; exit 1; }
verdict() { jq -r --arg n "$1" '.slos[] | select(.name == $n) | .verdict' report.json; }
[ "$(verdict checkout-availability)" = "EXHAUSTED" ] || fail "checkout should be EXHAUSTED"
[ "$(verdict search-availability)" = "HEALTHY" ] || fail "search should be HEALTHY"
[ "$(verdict billing-availability)" = "NO DATA" ] || fail "billing should be NO DATA"
jq -e '.slos[] | select(.name == "checkout-availability") | (.burnRates["1h"] > 19 and .burnRates["1h"] < 21)' report.json >/dev/null \
  || fail "checkout 1h burn rate should be about 20x"
jq -e '.slos[] | select(.name == "search-availability") | (.budgetRemaining > 89 and .budgetRemaining < 91)' report.json >/dev/null \
  || fail "search should have about 90% of its budget left"

set +e
$bin report --prometheus "$vm" e2e/slos.yaml >/dev/null
code=$?
set -e
[ "$code" -eq 1 ] || fail "an exhausted budget must exit 1, got $code"
echo "e2e: all expectations met"
