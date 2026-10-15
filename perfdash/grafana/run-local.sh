#!/usr/bin/env bash
# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PERFDASH_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORK_DIR="${WORK_DIR:-/tmp/perfdash-phase3}"
TSDB_DIR="${WORK_DIR}/prometheus"
CACHE_DIR="${WORK_DIR}/thanos-cache"
THANOS_BIN="${THANOS_BIN:-$(command -v thanos || echo "${GOPATH:-$HOME/go}/bin/thanos")}"
BUILDS="${BUILDS:-8}"

mkdir -p "${TSDB_DIR}" "${CACHE_DIR}"

if [ ! -f "${WORK_DIR}/config.yaml" ]; then
  cat << 'EOF' > "${WORK_DIR}/config.yaml"
periodics:
- name: ci-kubernetes-e2e-gce-scale-performance-5000
  tags:
  - "perfDashPrefix: gce-5000Nodes"
  - "perfDashJobType: performance"
- name: ci-kubernetes-e2e-gce-scale-performance-100
  tags:
  - "perfDashPrefix: gce-100Nodes"
  - "perfDashJobType: performance"
- name: ci-kubernetes-benchmark-write-throughput
  tags:
  - "perfDashPrefix: benchmark-write-throughput"
  - "perfDashJobType: benchmarkWriteThroughput"
- name: ci-kubernetes-benchmark-etcd-write-throughput
  tags:
  - "perfDashPrefix: benchmark-etcd-write-throughput"
  - "perfDashJobType: performance"
- name: ci-kubernetes-benchmark-list-json
  tags:
  - "perfDashPrefix: benchmark-list-json"
  - "perfDashJobType: benchmarkList"
- name: ci-kubernetes-benchmark-list-cbor
  tags:
  - "perfDashPrefix: benchmark-list-cbor"
  - "perfDashJobType: benchmarkList"
EOF
fi

cat << EOF > "${WORK_DIR}/bucket.yml"
type: FILESYSTEM
config:
  directory: "${TSDB_DIR}"
EOF

echo "Building perfdash..."
(cd "${PERFDASH_DIR}" && go build -o "${WORK_DIR}/perfdash" .)

echo "Starting thanos store (:10901 / :10903) and thanos query (:10902)..."
pkill -f "thanos store.*${WORK_DIR}/bucket.yml" 2>/dev/null || true
pkill -f "thanos query.*10902" 2>/dev/null || true

nohup "${THANOS_BIN}" store \
  --objstore.config-file="${WORK_DIR}/bucket.yml" \
  --data-dir="${CACHE_DIR}" \
  --sync-block-duration=10s \
  --store.enable-index-header-lazy-reader \
  --log.level=warn \
  --grpc-address=127.0.0.1:10901 \
  --http-address=0.0.0.0:10903 \
  > "${WORK_DIR}/thanos-store.log" 2>&1 &

nohup "${THANOS_BIN}" query \
  --http-address=0.0.0.0:10902 \
  --grpc-address=127.0.0.1:10904 \
  --endpoint=127.0.0.1:10901 \
  --query.lookback-delta=15m \
  --log.level=warn \
  > "${WORK_DIR}/thanos-query.log" 2>&1 &

echo "Starting perfdash web UI & ingester (:8080)..."
pkill -f "${WORK_DIR}/perfdash" 2>/dev/null || true
nohup "${WORK_DIR}/perfdash" \
  --www=true \
  --dir="${PERFDASH_DIR}/www" \
  --address=0.0.0.0:8080 \
  --mode=gcs \
  --builds="${BUILDS}" \
  --force-builds=true \
  --allow-parsers-matching-all-tests=false \
  --thanos-mode=true \
  --thanos-query-url=http://127.0.0.1:10902 \
  --thanos-tsdb-dir="${TSDB_DIR}" \
  --configPath="${WORK_DIR}/config.yaml" \
  > "${WORK_DIR}/perfdash.log" 2>&1 &

echo "Starting Grafana (:3000)..."
docker rm -f perfdash-grafana 2>/dev/null || true
docker run -d \
  --name perfdash-grafana \
  --network host \
  -e GF_SERVER_HTTP_ADDR=0.0.0.0 \
  -e GF_SERVER_HTTP_PORT=3000 \
  -e GF_AUTH_ANONYMOUS_ENABLED=true \
  -e GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer \
  -e GF_AUTH_DISABLE_LOGIN_FORM=true \
  -e GF_USERS_ALLOW_SIGN_UP=false \
  -e GF_SECURITY_ALLOW_EMBEDDING=true \
  -e GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH=/var/lib/grafana/dashboards/perfdash.json \
  -v "${SCRIPT_DIR}/provisioning:/etc/grafana/provisioning:ro" \
  -v "${SCRIPT_DIR}/dashboards:/var/lib/grafana/dashboards:ro" \
  grafana/grafana:6.2.0 >/dev/null

echo "Stack running:"
echo "  - Grafana:      http://0.0.0.0:3000/d/perfdash-grafana"
echo "  - Legacy UI:    http://0.0.0.0:8080/"
echo "  - Thanos Query: http://0.0.0.0:10902/"
