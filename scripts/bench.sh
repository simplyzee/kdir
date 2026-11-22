#!/usr/bin/env bash
# Benchmark kdir's prompt-hook paths with hyperfine on a kubeconfig with 50
# contexts. Usage: scripts/bench.sh [output.md]
set -euo pipefail

command -v hyperfine >/dev/null || { echo "bench: hyperfine is required" >&2; exit 1; }
repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/kdir-bench.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
out=${1:-}

bin="$work/bin/kdir"
(cd "$repo" && go build -trimpath -ldflags '-s -w' -o "$bin" ./cmd/kdir)

export HOME="$work/home" XDG_RUNTIME_DIR="$work/run" XDG_CONFIG_HOME="$work/config"
unset KUBECONFIG
mkdir -p "$HOME/.kube"
{
  printf 'apiVersion: v1\nkind: Config\ncurrent-context: ctx-0\nclusters:\n'
  for i in $(seq 0 49); do printf -- '- name: cluster-%d\n  cluster:\n    server: https://cluster-%d.example.com:6443\n    certificate-authority-data: ZmFrZQ==\n' "$i" "$i"; done
  printf 'users:\n'
  for i in $(seq 0 49); do printf -- '- name: user-%d\n  user:\n    token: fake-%d\n' "$i" "$i"; done
  printf 'contexts:\n'
  for i in $(seq 0 49); do printf -- '- name: ctx-%d\n  context:\n    cluster: cluster-%d\n    user: user-%d\n    namespace: ns-%d\n' "$i" "$i" "$i" "$i"; done
} > "$HOME/.kube/config"

# A project six levels below its .kubectx, matched by server URL (the matcher
# that has to look at every context).
proj="$HOME/src/team/payments"
deep="$proj/services/api/internal/handlers/v2/tests"
mkdir -p "$deep" "$HOME/src/other/repo"
printf 'server: https://cluster-42.example.com:6443\nnamespace: payments\n' > "$proj/.kubectx"
"$bin" allow "$proj" 2>/dev/null

pid=424242
clean_env() { unset KDIR_STATE KDIR_OVERLAY KDIR_ORIG_KUBECONFIG KDIR_ORIG_KUBECONFIG_SET KDIR_ROOT KDIR_CONTEXT KDIR_NAMESPACE KDIR_PROTECT KUBECONFIG; }
inside_env() { (cd "$deep" && "$bin" export bash --pid "$pid"); }

results=()
# bench NAME DIR STATE(inside|clean) ARGS...: hyperfine runs kdir directly
# (no shell wrapper), in DIR, with the environment a hooked shell would have.
bench() {
  local name=$1 dir=$2 st=$3; shift 3
  local md="$work/$(printf '%s' "$name" | tr -c 'a-z0-9' '_').md"
  (
    clean_env
    [ "$st" = inside ] && eval "$(inside_env)"
    cd "$dir"
    hyperfine -N --warmup 30 --min-runs 500 --style none --export-markdown "$md" -n "$name" "$*" >/dev/null
  )
  results+=("$md")
}

bench 'not governed'                       "$HOME/src/other/repo" clean  "$bin" export bash --pid $pid
bench 'governed, nothing changed (fast path)' "$deep"             inside "$bin" export bash --pid $pid
bench 'switch: enter project'              "$deep"                clean  "$bin" export bash --pid $pid
bench 'switch: leave project'              "$HOME/src/other/repo" inside "$bin" export bash --pid $pid
bench 'status --short'                     "$deep"                inside "$bin" status --short
bench 'baseline: kdir version'             "$deep"                clean  "$bin" version

{
  echo "kdir $("$bin" version | cut -d' ' -f2), $(uname -sm), 50-context kubeconfig, .kubectx 6 levels up, server matcher"
  echo
  echo '| Path | Mean ± σ [ms] |'
  echo '|:---|---:|'
  for md in "${results[@]}"; do tail -n +3 "$md" | awk -F'|' '{ printf "|%s|%s|\n", $2, $3 }'; done
} > "$work/table.md"
cat "$work/table.md"
[ -n "$out" ] && cp "$work/table.md" "$out"
exit 0
