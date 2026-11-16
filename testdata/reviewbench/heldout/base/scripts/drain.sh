#!/usr/bin/env bash
set -euo pipefail
# drain sends any queued events before maintenance
notifier --timeout 10
