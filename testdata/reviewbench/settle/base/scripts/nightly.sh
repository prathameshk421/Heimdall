#!/usr/bin/env bash
# Cron runs this at 01:00 UTC. The rehearsal must pass before money moves.
set -euo pipefail
./settle --dry-run
./settle
