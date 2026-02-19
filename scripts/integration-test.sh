#!/usr/bin/env bash
# Integration test suite for onchain-monitor API.
# Expects the server to be running at BASE_URL (default http://localhost:8080).
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
PASS=0
FAIL=0
TOTAL=0

red()   { printf '\033[0;31m%s\033[0m\n' "$*"; }
green() { printf '\033[0;32m%s\033[0m\n' "$*"; }

assert_status() {
  local desc="$1" method="$2" url="$3" expected="$4"
  shift 4
  TOTAL=$((TOTAL + 1))
  local status
  status=$(curl -sf -o /dev/null -w "%{http_code}" -X "$method" "$@" "$url" 2>/dev/null || curl -o /dev/null -w "%{http_code}" -X "$method" "$@" "$url" 2>/dev/null)
  if [ "$status" = "$expected" ]; then
    green "  ✓ $desc (HTTP $status)"
    PASS=$((PASS + 1))
  else
    red   "  ✗ $desc (expected $expected, got $status)"
    FAIL=$((FAIL + 1))
  fi
}

assert_json_field() {
  local desc="$1" url="$2" jq_expr="$3" expected="$4"
  TOTAL=$((TOTAL + 1))
  local actual
  actual=$(curl -sf "$url" 2>/dev/null | jq -r "$jq_expr" 2>/dev/null || echo "CURL_FAILED")
  if [ "$actual" = "$expected" ]; then
    green "  ✓ $desc ($actual)"
    PASS=$((PASS + 1))
  else
    red   "  ✗ $desc (expected '$expected', got '$actual')"
    FAIL=$((FAIL + 1))
  fi
}

assert_json_gt() {
  local desc="$1" url="$2" jq_expr="$3" min="$4"
  TOTAL=$((TOTAL + 1))
  local actual
  actual=$(curl -sf "$url" 2>/dev/null | jq -r "$jq_expr" 2>/dev/null || echo "0")
  if [ "$(echo "$actual > $min" | bc -l 2>/dev/null)" = "1" ]; then
    green "  ✓ $desc ($actual > $min)"
    PASS=$((PASS + 1))
  else
    red   "  ✗ $desc (expected > $min, got $actual)"
    FAIL=$((FAIL + 1))
  fi
}

echo "═══════════════════════════════════════════"
echo " Onchain Monitor — Integration Tests"
echo " Target: $BASE_URL"
echo "═══════════════════════════════════════════"

# ── Health ────────────────────────────────────
echo ""
echo "▸ Health Endpoints"
assert_json_field "GET /healthz returns ok" \
  "$BASE_URL/healthz" '.status' 'ok'

assert_json_field "GET /readyz returns ready" \
  "$BASE_URL/readyz" '.status' 'ready'

assert_status "GET /metrics returns prometheus data" \
  GET "$BASE_URL/metrics" 200

# ── Events ────────────────────────────────────
echo ""
echo "▸ Events API"
assert_status "GET /api/events returns 200" \
  GET "$BASE_URL/api/events" 200

assert_json_gt "Events list is non-empty" \
  "$BASE_URL/api/events" 'length' 0

# Verify known seeded events exist
for evt in altura_metric_alert general_metric_alert general_binance_price_alert general_merkl_alert general_turtle_alert; do
  assert_json_gt "Event '$evt' is seeded" \
    "$BASE_URL/api/events" "[.[] | select(.name==\"$evt\")] | length" 0
done

# ── Stats ─────────────────────────────────────
echo ""
echo "▸ Stats API"
assert_status "GET /api/stats returns 200" \
  GET "$BASE_URL/api/stats" 200

assert_status "GET /api/stats/meta returns 200" \
  GET "$BASE_URL/api/stats/meta" 200

assert_json_field "Stats meta poll_interval is 60s" \
  "$BASE_URL/api/stats/meta" '.poll_interval' '60s'

# ── Subscriptions ─────────────────────────────
echo ""
echo "▸ Subscriptions CRUD"

# Missing param → 400
assert_status "GET /api/subscriptions without tg_chat_id → 400" \
  GET "$BASE_URL/api/subscriptions" 400

# Valid empty list
assert_status "GET /api/subscriptions with unknown user → 200" \
  GET "$BASE_URL/api/subscriptions?tg_chat_id=999999" 200

# Get first event ID for subscription test
EVENT_ID=$(curl -sf "$BASE_URL/api/events" | jq '.[0].id' 2>/dev/null || echo "1")

# Create subscription
TOTAL=$((TOTAL + 1))
SUB_RESP=$(curl -sf -X POST "$BASE_URL/api/subscriptions" \
  -H "Content-Type: application/json" \
  -d "{\"tg_chat_id\":12345,\"event_id\":$EVENT_ID,\"threshold_pct\":5,\"direction\":\"drop\"}" 2>/dev/null || echo "")
SUB_ID=$(echo "$SUB_RESP" | jq -r '.id' 2>/dev/null || echo "")
if [ -n "$SUB_ID" ] && [ "$SUB_ID" != "null" ]; then
  green "  ✓ POST /api/subscriptions creates subscription (id=$SUB_ID)"
  PASS=$((PASS + 1))
else
  red   "  ✗ POST /api/subscriptions failed to create subscription"
  FAIL=$((FAIL + 1))
fi

# List subscription for that user
if [ -n "$SUB_ID" ] && [ "$SUB_ID" != "null" ]; then
  assert_json_gt "Subscription appears in user list" \
    "$BASE_URL/api/subscriptions?tg_chat_id=12345" 'length' 0

  # Update subscription
  assert_status "PUT /api/subscriptions/$SUB_ID updates" \
    PUT "$BASE_URL/api/subscriptions/$SUB_ID" 200 \
    -H "Content-Type: application/json" \
    -d '{"threshold_pct":15,"direction":"increase"}'

  # Delete subscription
  assert_status "DELETE /api/subscriptions/$SUB_ID removes" \
    DELETE "$BASE_URL/api/subscriptions/$SUB_ID" 204

  # Verify deleted
  assert_json_field "Subscription list is empty after delete" \
    "$BASE_URL/api/subscriptions?tg_chat_id=12345" 'length' '0'
fi

# ── Notifications ─────────────────────────────
echo ""
echo "▸ Notifications API"
assert_status "GET /api/notifications without tg_chat_id → 400" \
  GET "$BASE_URL/api/notifications" 400

assert_status "GET /api/notifications with valid user → 200" \
  GET "$BASE_URL/api/notifications?tg_chat_id=12345" 200

# ── Link / Unlink ─────────────────────────────
echo ""
echo "▸ Link/Unlink API"
assert_status "POST /api/link without code → 400" \
  POST "$BASE_URL/api/link" 400 \
  -H "Content-Type: application/json" -d '{}'

assert_status "POST /api/link with bad code → 404" \
  POST "$BASE_URL/api/link" 404 \
  -H "Content-Type: application/json" -d '{"code":"invalid-code"}'

assert_status "POST /api/unlink without tg_chat_id → 400" \
  POST "$BASE_URL/api/unlink" 400 \
  -H "Content-Type: application/json" -d '{}'

# ── CORS ──────────────────────────────────────
echo ""
echo "▸ CORS"
TOTAL=$((TOTAL + 1))
CORS_HEADER=$(curl -sf -I -X OPTIONS "$BASE_URL/api/events" \
  -H "Origin: http://example.com" \
  -H "Access-Control-Request-Method: GET" 2>/dev/null | grep -i 'access-control-allow-origin' || echo "")
if [ -n "$CORS_HEADER" ]; then
  green "  ✓ OPTIONS returns CORS headers"
  PASS=$((PASS + 1))
else
  red   "  ✗ OPTIONS missing CORS headers"
  FAIL=$((FAIL + 1))
fi

# ── Summary ───────────────────────────────────
echo ""
echo "═══════════════════════════════════════════"
if [ "$FAIL" -eq 0 ]; then
  green " ALL $TOTAL TESTS PASSED ✓"
else
  red   " $FAIL/$TOTAL TESTS FAILED"
fi
echo "═══════════════════════════════════════════"

exit "$FAIL"
