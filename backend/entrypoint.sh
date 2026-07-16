#!/bin/sh
# Materialize the operator keypair from a secret env var (hosting platforms
# store secrets as env, not files). OPERATOR_KEYPAIR_JSON is the JSON array
# from ~/.config/solana/id.json.
set -e
cd /data 2>/dev/null || cd /app
if [ -n "$OPERATOR_KEYPAIR_JSON" ] && [ -z "$OPERATOR_KEYPAIR" ]; then
  printf '%s' "$OPERATOR_KEYPAIR_JSON" > /data/operator.json
  chmod 600 /data/operator.json
  export OPERATOR_KEYPAIR=/data/operator.json
fi
exec /app/server
