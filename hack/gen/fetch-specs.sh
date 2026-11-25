#!/usr/bin/env bash
# Copyright 2026 High Five Labs
# SPDX-License-Identifier: Apache-2.0
# Download the vendor's OpenAPI specs at the commits vendor.yaml pins.
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p specs
[ -s specs/pagerduty.json ] || curl -fsSL -o specs/pagerduty.json "https://raw.githubusercontent.com/PagerDuty/api-schema/3fe48a2b037cad384edd1ae4f51645eca145faa5/reference/REST/openapiv3.json"
for f in integration-jira-cloud integration-slack-service; do
  [ -s "specs/$f.json" ] || curl -fsSL -o "specs/$f.json" "https://raw.githubusercontent.com/PagerDuty/api-schema/3fe48a2b037cad384edd1ae4f51645eca145faa5/reference/$f/openapiv3.json"
done
