#!/usr/bin/env sh
set -eu
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_root"
set -a
if [ -f .env ]; then . ./.env; else . ./.env.example; fi
set +a
(command -v jq >/dev/null 2>&1) || { echo "jq is required for API validation" >&2; exit 1; }
(cd backend && go test ./... && go build ./...)
(cd frontend && npm install --no-audit --no-fund && npm run build)
docker compose config --quiet
docker compose up -d --build
cleanup() { docker compose down -v --remove-orphans; }
if [ "${KEEP_RUNNING:-0}" = "1" ]; then
  trap cleanup INT TERM
else
  trap cleanup EXIT INT TERM
fi
i=0
until curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19517}/healthz" >/dev/null; do
  i=$((i+1)); [ "$i" -lt 60 ] || { docker compose logs; exit 1; }; sleep 2
done
curl -fsS "http://127.0.0.1:${FRONTEND_PORT:-18517}/" >/dev/null
login_token() {
  curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19517}/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"Admin123!\"}" | jq -er '.data.token'
}
token=$(login_token admin)
viewer_token=$(login_token viewer)
operator_token=$(login_token operator)
reviewer_token=$(login_token reviewer)
[ -n "$token" ]
curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19517}/api/overview" -H "Authorization: Bearer $token" >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/session" -H "Authorization: Bearer $token" | jq -e '.data.role == "admin" and (.data.requestId | length > 0)' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/runtime" -H "Authorization: Bearer $token" | jq -e '.data.appName and .data.databaseDriver and (.data.requestLimit > 0)' >/dev/null
paths=$(sed -n "s/.*path: '\\([^']*\\)'.*/\\1/p" frontend/src/types/status.ts)
for path in $paths; do
  curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/$path?page=1&pageSize=20" -H "Authorization: Bearer $token" | jq -e '.data | type == "array"' >/dev/null
done
entity_config=$(sed -n "s/.*path: '\\([^']*\\)'.*statuses: \\['\\([^']*\\)', '\\([^']*\\)'.*/\\1|\\2|\\3/p" frontend/src/types/status.ts | head -n 1)
resource=$(printf '%s' "$entity_config" | cut -d '|' -f 1)
initial_status=$(printf '%s' "$entity_config" | cut -d '|' -f 2)
next_status=$(printf '%s' "$entity_config" | cut -d '|' -f 3)
now=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
code="SMOKE-$(date +%s)"
payload=$(printf '{"code":"%s","name":"Runtime smoke record","description":"Automated Compose workflow validation","facility":"Validation Lab","owner":"admin","category":"smoke","riskLevel":"low","metricValue":1,"metricUnit":"unit","effectiveAt":"%s","evidence":"scripts/validate.sh","relatedCode":"SMOKE"}' "$code" "$now")
created=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/$resource" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$payload")
id=$(printf '%s' "$created" | jq -er '.data.id')
version=$(printf '%s' "$created" | jq -er '.data.version')
printf '%s' "$created" | jq -e --arg status "$initial_status" '.data.status == $status' >/dev/null
transition=$(printf '{"status":"%s","expectedVersion":%s,"reason":"automated runtime validation"}' "$next_status" "$version")
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/$resource/$id/transition" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$transition" | jq -e --arg status "$next_status" '.data.status == $status' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/audits?page=1&pageSize=100" -H "Authorization: Bearer $token" | jq -e '.meta.total >= 2' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/audit-summary?windowHours=24" -H "Authorization: Bearer $token" | jq -e '.data.total >= 2 and .data.transitions >= 1' >/dev/null

# A viewer may inspect operations but may never mutate them or inspect audits.
viewer_write_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/runs" \
  -H "Authorization: Bearer $viewer_token" -H 'Content-Type: application/json' -d "$payload")
[ "$viewer_write_status" = "403" ]
viewer_audit_status=$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:${BACKEND_PORT}/api/audits" -H "Authorization: Bearer $viewer_token")
[ "$viewer_audit_status" = "403" ]

# Release decisions are versioned and only reviewer/admin may cross the release gate.
decision_code="RD-SMOKE-$(date +%s)"
decision_payload=$(printf '{"code":"%s","name":"Runtime release gate","description":"RBAC and immutable revision validation","facility":"Validation Lab","owner":"operator","category":"calibration","riskLevel":"medium","metricValue":2.2,"metricUnit":"dE","effectiveAt":"%s","evidence":"spectrophotometer validation evidence","relatedCode":"PR-001"}' "$decision_code" "$now")
decision=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/release" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: release-create-smoke' -d "$decision_payload")
decision_id=$(printf '%s' "$decision" | jq -er '.data.id')
decision_version=$(printf '%s' "$decision" | jq -er '.data.version')
release_payload=$(printf '{"status":"release","expectedVersion":%s,"reason":"validated proof and colour tolerance"}' "$decision_version")
operator_release_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/release/$decision_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$release_payload")
[ "$operator_release_status" = "403" ]
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/release/$decision_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -H 'X-Request-ID: release-review-smoke' -d "$release_payload" | jq -e '.data.status == "release" and .data.version == 2' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/release/$decision_id" -H "Authorization: Bearer $reviewer_token" | jq -e '.data.revisions | length == 2 and .[0].requestId == "release-review-smoke" and .[1].requestId == "release-create-smoke"' >/dev/null

# Proof capture is operational work; accepting the proof is a reviewer action.
# Each proof records three ΔE positions: operation side, centre, drive side.
proof_code="CP-SMOKE-$(date +%s)"
proof_run_code="PR-SMOKE-$(date +%s)"
proof_run_payload=$(printf '{"code":"%s","name":"Proof gate run","description":"Three-position proof gate","facility":"Validation Lab","owner":"operator","category":"calibration","riskLevel":"medium","metricUnit":"dE","colorTolerance":3.0,"effectiveAt":"%s","evidence":"run for proof gate","relatedCode":"REL-SMOKE"}' "$proof_run_code" "$now")
proof_run=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/runs" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$proof_run_payload")
proof_run_id=$(printf '%s' "$proof_run" | jq -er '.data.id')
proof_run_version=$(printf '%s' "$proof_run" | jq -er '.data.version')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/runs/$proof_run_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"printing","expectedVersion":%s,"reason":"plates verified"}' "$proof_run_version")" >/dev/null
proof_run_version=$((proof_run_version + 1))
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/runs/$proof_run_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"proofing","expectedVersion":%s,"reason":"proof strip ready"}' "$proof_run_version")" >/dev/null

# 1) A proof missing one position cannot be submitted for review (422), and the
#    batch is held at proofing with the missing-reading reason.
proof_payload=$(printf '{"code":"%s","name":"Runtime proof gate","description":"Proof acceptance validation","facility":"Validation Lab","owner":"operator","category":"calibration","riskLevel":"low","metricUnit":"dE","effectiveAt":"%s","evidence":"proof strip measurements","relatedCode":"%s","operationSide":1.4,"center":1.8}' "$proof_code" "$now" "$proof_run_code")
proof=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/proofs" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$proof_payload")
proof_id=$(printf '%s' "$proof" | jq -er '.data.id')
proof_version=$(printf '%s' "$proof" | jq -er '.data.version')
missing_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"review","expectedVersion":%s,"reason":"drive side still missing"}' "$proof_version")")
[ "$missing_status" = "422" ]
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/runs/$proof_run_id" -H "Authorization: Bearer $operator_token" \
  | jq -e '.data.status == "hold" and (.data.holdReason | length > 0)' >/dev/null

# 2) Complete the readings, but with the worst (drive) position over the batch
#    tolerance; reviewer acceptance must fail (422) and keep the run held.
proof_update=$(printf '{"expectedVersion":%s,"name":"Runtime proof gate","facility":"Validation Lab","owner":"operator","category":"calibration","riskLevel":"low","metricUnit":"dE","effectiveAt":"%s","evidence":"proof strip measurements","relatedCode":"%s","operationSide":1.4,"center":1.8,"driveSide":3.9}' "$proof_version" "$now" "$proof_run_code")
proof=$(curl -fsS -X PUT "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$proof_update")
proof_version=$(printf '%s' "$proof" | jq -er '.data.version')
proof=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"review","expectedVersion":%s,"reason":"measurement capture completed"}' "$proof_version")")
proof_version=$(printf '%s' "$proof" | jq -er '.data.version')
over_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"accepted","expectedVersion":%s,"reason":"worst drive side exceeds batch tolerance"}' "$proof_version")")
[ "$over_status" = "422" ]

# 3) Re-measure within tolerance. A new judgment version is created, the old
#    conclusions remain in history, and re-review clears the proof gate.
proof_update=$(printf '{"expectedVersion":%s,"name":"Runtime proof gate","facility":"Validation Lab","owner":"operator","category":"calibration","riskLevel":"low","metricUnit":"dE","effectiveAt":"%s","evidence":"proof strip re-measured","relatedCode":"%s","operationSide":1.3,"center":1.6,"driveSide":1.9}' "$proof_version" "$now" "$proof_run_code")
proof=$(curl -fsS -X PUT "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$proof_update")
proof_version=$(printf '%s' "$proof" | jq -er '.data.version')
printf '%s' "$proof" | jq -e '.data.status == "captured" and .data.worstPosition == "drive" and .data.metricValue == 1.9' >/dev/null
proof=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"review","expectedVersion":%s,"reason":"re-measurement completed"}' "$proof_version")")
proof_version=$(printf '%s' "$proof" | jq -er '.data.version')
operator_accept_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"accepted","expectedVersion":%s,"reason":"operator must not accept"}' "$proof_version")")
[ "$operator_accept_status" = "403" ]
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' \
  -d "$(printf '{"status":"accepted","expectedVersion":%s,"reason":"worst position within batch tolerance"}' "$proof_version")" \
  | jq -e '.data.status == "accepted" and .data.worstPosition == "drive"' >/dev/null
# Active judgment is the latest accepted version; older rounds stay in history.
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/proofs/$proof_id" -H "Authorization: Bearer $reviewer_token" \
  | jq -e '.data.judgments | length == 3 and .[0].active == true and .[0].conclusion == "accepted" and .[1].active == false and .[2].active == false' >/dev/null
# The batch returns to proofing and the blocking reason is cleared.
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/runs/$proof_run_id" -H "Authorization: Bearer $reviewer_token" \
  | jq -e '.data.status == "proofing" and .data.holdReason == ""' >/dev/null
docker compose ps
[ "${KEEP_RUNNING:-0}" = "1" ] && echo "KEEP_RUNNING=1: containers left running for browser validation"
