#!/bin/bash
# Clear all alert dedup keys for a specific Telegram chat ID.
# Usage: ./clear-dedup.sh <chat_id> [--dry-run]
#
# Examples:
#   ./clear-dedup.sh 123456789              # delete all dedup keys
#   ./clear-dedup.sh 123456789 --dry-run    # list keys without deleting

set -euo pipefail

CHAT_ID="${1:?Usage: $0 <chat_id> [--dry-run]}"
DRY_RUN="${2:-}"

REDIS_PW=$(kubectl get secret -n redis redis-password -o jsonpath='{.data.password}' | base64 -d)

echo "Looking up dedup keys for chat ID: $CHAT_ID ..."
KEYS=$(kubectl exec -n redis redis-0 -- redis-cli -a "$REDIS_PW" --no-auth-warning KEYS "*${CHAT_ID}*" 2>/dev/null)

if [ -z "$KEYS" ]; then
    echo "No dedup keys found for chat ID $CHAT_ID"
    exit 0
fi

COUNT=$(echo "$KEYS" | wc -l)
echo "Found $COUNT key(s):"
echo "$KEYS" | sed 's/^/  /'

if [ "$DRY_RUN" = "--dry-run" ]; then
    echo ""
    echo "(dry run — no keys deleted)"
    exit 0
fi

echo ""
echo "Deleting..."
echo "$KEYS" | while read -r key; do
    kubectl exec -n redis redis-0 -- redis-cli -a "$REDIS_PW" --no-auth-warning DEL "$key" >/dev/null 2>&1
done

echo "Done — cleared $COUNT dedup key(s) for chat ID $CHAT_ID"
