#!/bin/bash
# secret-4 v1.27.2 install for a node that did not upgrade at the halt.
#
#   cd mainnet && I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes ./post-launch.sh install
#
# Use this instead of autopilot.sh once the chain is producing blocks on 1.27.2.
# autopilot.sh runs only while secret-4-v1.27.2 is the current upgrade. After the
# collector marks it done, require_current exits and the combined file endpoint
# answers 410, so autopilot.sh install can no longer fetch anything.
#
# The combined file is frozen by then, so it is carried below instead of fetched.
# That is the only difference. This writes the same file to the same place and
# then execs the same halt.sh --install, which runs the handover on 1.26.0 and
# installs the package.
#
# The node must still be running 1.26.0 and must have halted on plan v1.27.2.
# halt.sh reads SECRETD_HOME, SERVICE_UNIT_FILE and SCRT_SGX_STORAGE. SERVICE is
# picked here: cosmovisor is stopped when that unit exists, then secret-node is
# used when that unit exists and cosmovisor otherwise.
set -euo pipefail
cd "$(dirname "$0")"

# install is the only subcommand. A typo must not fall through to the handover.
[[ "${1:-install}" == "install" ]] || { echo "usage: $0 install"; exit 1; }

[[ -f ./halt.sh ]] || { echo "put post-launch.sh next to halt.sh"; exit 1; }
# A kit fetched with curl has no exec bit.
chmod +x ./halt.sh ./check-hw/check-hw

# halt.sh checks this too, but stop before touching the SGX store if it is wrong.
ver="$(secretd version 2>/dev/null | head -1 || true)"
[[ "$ver" == "1.26.0" ]] || { echo "secretd is ${ver:-empty}, want 1.26.0"; exit 1; }

store="${SCRT_SGX_STORAGE:-/opt/secret/.sgx_secrets}"
sudo mkdir -p "$store"

# The combined file the collector froze for secret-4-v1.27.2: 7 signatures.
base64 -d > /tmp/migration_consensus.json <<'B64'
eyIyREQwOThDOEVDQUYwNERGRTMxQkJDNTk3OTlDNzg2QUMwOUJGNTNGIjpbIkZB
d1J2UUtpU1o0Zm1Hai92eHJESm1tSU12MVpEN2hCYjdWWTZWZUtJbDA9IiwieUV3
Q2YwdTgyL2QwWmk2QjM4VDZnVWgrS2RlNkt5SkhXR0VmNnpLcS91cld3RUN0SlhP
MjNPdXNXZnFlOWZ3eUdpNFRhU0xydkc5UjVIUXNJN21DQlE9PSJdLCI0Q0NFNTYy
QjFFMkJDNTcxNzUxREI1MTIyMjJDRUQ1QTA4MjQ3MEVBIjpbIndaSk1QWUtjMjRk
emxGSzdHeGhyYnJya0Z3U1NJbzUzVTM1NzNMcWE3bzg9IiwiYklYK3FrYVQyQ0My
bmNZOUF3OFZhYS9ub2ZkNWEyWmQraUNGVVRTNktLWUtkVG42K3hTanlFdWRpMTZQ
M2k2QXh6bTJOaENJVi8veGdnM3Uydm1YQ3c9PSJdLCI3M0Q5RERDOUVCQjVCREI0
NEFEQTlGRjIwNTE2MTBCNzVDQjMxQThEIjpbIk9rbnlDOGE2SE10b0lrSEhLQ2lh
Zzc5NXhGY1k3Vk9IazBXNk1wRzkvOWs9IiwiclJhTGd4c29Yd052TVpNK2RtaFFy
T2ZidkMrZWwrZTErQ1lkNUdpL1ZZY3IzckJucjJFWmZYSGNnV1NNZlpMZU1LU3I4
SkVkd0wvVk45OW9OMEljQWc9PSJdLCI4MUVCQ0UyRkZDMjk4MjAzNTFDMDg2RTlF
REE2QTIyMDA5OEZGNDFDIjpbIjIxeEVya0ZLVXllTVY5SmNmeUFqcmUyZ01CQmIv
MEdOU2t2ayt0cDVKMlk9IiwiYWtTeU9nOU9OSEI4bHg2QlljUndUS042U0dYQnVu
QlVTWnhqSldFWC9nMHhINUpwdW11TTNsV1lPckV4NVZCOVpyQUZWYzdZM3Y4WjBl
Z0ptMk1DQ1E9PSJdLCJENjBENUVFNTlDRjdCMUYwRDc1NUZEMTY3OTY2MUY0MkND
MDNDREREIjpbInBtaytXZ1JHT2NZbGMrSjJDa2xyNjNmanFYcStSQnl4a2hsV3RU
WVFvY1E9IiwidGJCN2xCTmUwQmtWc0pkSjIvUHIvMm9hMzVvNm5nWkcvSTlMRVdx
U0NJQUZBeFRHSXJINmtPY3lTMHhDYmk2RllJcThzWGh6RVFCTk4wMmc4eE5QQ1E9
PSJdLCJFNTJFNTJCOTNENUYwMTMyRkZCMTU3QjU5NzEzODk3RUExQzkzMjQxIjpb
IkltcStUZ0svdElhdHhZbEtkb2c3MkdGTmlxK2FrNUZ2VDVldDNvdXBydWs9Iiwi
VjB3R2NGT21FeXRsUy9QUnJncjBwb1JHeS9lSVNSUW1OTmo5U3JYcVA2ZmhVZllh
VGtQeGVILzdPWlRBRW51ZkxKeXZMR1JBVXp1QzhOQkVEK0JNQ2c9PSJdLCJFODU1
MTA5QjIxMkI5RUI2NUM5ODJGRDQ0RUUxM0U3N0U5RTMzQzRBIjpbIlc1M3Y5K1FN
MWxTNHVZYmNmV3c4dVBBb1FBMVI2THRUcUxNeVNUZTI0b3M9IiwiUHh5bkdTR2Ro
bFdvbzJiZHArZjNSclhQNkU5R2xtODlKaVFvTTloa0ttMG5BeUhiT2RpL2RXcklY
YklWZ0srYlhTQWxoemR0K0lqd3pvSk9FbU4xQnc9PSJdfQo=
B64
python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); assert len(d)==7' /tmp/migration_consensus.json
sudo cp /tmp/migration_consensus.json "$store/migration_consensus.json"
sudo chown "$(id -u):$(id -g)" "$store/migration_consensus.json"

# cosmovisor restarts the node on its own and would fight the handover.
if systemctl cat cosmovisor.service >/dev/null 2>&1; then
  sudo systemctl stop cosmovisor.service || true
  sudo systemctl disable cosmovisor.service || true
fi
if systemctl cat secret-node.service >/dev/null 2>&1; then
  export SERVICE=secret-node
elif systemctl cat cosmovisor.service >/dev/null 2>&1; then
  export SERVICE=cosmovisor
else
  echo "no secret-node or cosmovisor unit"
  exit 1
fi

export I_UNDERSTAND=yes I_CONFIRM_PRECHECK=yes
exec ./halt.sh --install
