#!/usr/bin/env bash
source "$(dirname "$0")/common.sh"
# Migration explicitly targets the deployment project, never the local emulator.
# Pass --dry-run to print the changes without writing them.
env -u FIRESTORE_EMULATOR_HOST go run ./cmd/migrate-titles --project="$PROJECT_ID" "$@"
