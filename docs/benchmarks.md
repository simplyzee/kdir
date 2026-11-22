# Benchmarks

Measured with `scripts/bench.sh` (hyperfine, 500+ runs each, kdir run directly with no shell wrapper).

kdir dev, Darwin arm64, 50-context kubeconfig, .kubectx 6 levels up, server matcher

| Path | Mean ± σ [ms] |
|:---|---:|
| `not governed` | 5.6 ± 0.4 |
| `governed, nothing changed (fast path)` | 5.8 ± 0.4 |
| `switch: enter project` | 7.9 ± 0.4 |
| `switch: leave project` | 5.5 ± 0.4 |
| `status --short` | 6.0 ± 0.4 |
| `baseline: kdir version` | 5.4 ± 0.4 |
