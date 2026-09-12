# forecast-warden

**Rule-based CLI for forecast pipeline ops:** synthetic metrics → pluggable detectors → Forecast Incident (markdown + JSON) → human resolve.

MIT · **Go** (single static binary) · No LLM · No UI · No real company data.

> **Language decision:** fatia 1 originally shipped in Python; ported to **Go** for a single distributable binary and easier ops/adoption in CI and on-call laptops (no venv/pip). Product contracts (CSV, incidents, exit codes) stay stable; detectors are now **pluggable via `warden.yaml`**.

## Problem

Forecast teams invest in *models* and under-invest in *cycle management*: drift, bias, broken features, calendar effects. Knowledge lives in Slack. There is no standard artifact (incident) and no human gate before promote/retrain.

## Ritual (more important than the code)

1. After the batch: `warden check`
2. If any finding ≥ `warning`: an `open` Forecast Incident is written
3. Weekly triage (15 min): humans review `open` incidents
4. **Gate:** promote / change config only after critical incidents are `resolved` or `accepted-risk`
5. Close the loop with `warden resolve <id> --note "..."`

## Quickstart (5-minute demo)

```bash
git clone https://github.com/luizpedrini/forecast-warden.git
cd forecast-warden
go build -o warden ./cmd/warden

./warden init          # config + ~14d synthetic zone metrics (some sick) + baseline
./warden check         # writes ≥1 incident under incidents/ (exit 2 if critical)
./warden list
./warden show <id>     # use id from list, e.g. fw-20260912-xxxx
./warden resolve <id> --note "reviewed; hold promotion until retune"
```

Exit codes (CI-friendly): `0` ok · `1` warning · `2` critical.

Demo sick zones (latest run): **Z3** critical MAPE/WAPE + bias + coverage; **Z7** warning MAPE/WAPE + bias; **Z5** low support (info only).

## Pluggable metrics & detectors

Metrics CSV is **wide `metrics_v2`**: stable identity columns plus optional float metrics. Unknown / non-float columns are ignored. Detectors that reference a **missing** metric produce **no finding** (no crash) — so old CSVs without `wape`/`rmse` keep working.

| Stable columns | Optional metrics (examples) |
|----------------|----------------------------|
| `run_id`, `entity_type`, `entity_id`, `n_actuals`, `generated_at` | `mape`, `bias`, `coverage_80`, **`wape`**, **`rmse`**, … |

Configure detectors in `warden.yaml` (`schema_version: 2`):

| Type | Role |
|------|------|
| `threshold` | Alert when metric is `above` or `below` warning/critical |
| `zscore` | Alert on `\|value\| > abs_warning` **or** `\|z\| > z_warning` vs baseline |
| `low_support` | `n_actuals < min_support` (default severity `info`) |

### Default detectors

| Detector | Rule | Severity |
|----------|------|----------|
| **HighMAPE** | `mape > 0.25` & `n ≥ 30` → warning; `> 0.40` → critical | warning / critical |
| **HighWAPE** | `wape > 0.30` / `> 0.45` (logistics-opinionated; volume-weighted) | warning / critical |
| **HighRMSE** | `rmse > 10` / `> 20` | warning / critical |
| **BiasShift** | `\|bias\| > 0.15` **or** `\|z\| > 3` vs baseline | warning |
| **LowSupport** | `n < 30` | **info only** (alone does **not** open an incident) |
| **CoverageBreak** | `coverage_80 < 0.60` & `n ≥ 30` (`direction: below`) | warning |

**WAPE** is a logistics-friendly default option: when volumes vary across SKUs/zones, MAPE can overweight tiny series; WAPE weights by actual volume. Keep HighMAPE, add HighWAPE, or swap — it's config, not a rewrite.

Baseline is **long** (`entity_id`, `metric`, `mean`, `std`); legacy wide `mape_mean`/`bias_mean` files still load.

An incident opens only if there is ≥1 warning/critical finding. Re-running `check` does **not** clobber terminal statuses (`resolved` / `accepted-risk` / `wontfix`).

## CLI

```
warden init
warden check [--run-id YYYY-MM-DD] [--metrics PATH]
warden list [--status open|resolved|accepted-risk|wontfix]
warden show <id>
warden resolve <id> --note "..." [--status resolved|accepted-risk|wontfix]
```

## Non-goals

- Training or serving models / computing metrics from y/yhat raw
- Feature store / Feast / long-format required
- Required LLM narration
- Fleet / promise integration
- Web UI
- Real company data or IP

See [SPEC.md](SPEC.md) for the full contract (fatia 1 + pluggable metrics).

## Develop

```bash
go test ./...
go build -o warden ./cmd/warden
```

## Changelog

- **0.2.0:** Pluggable metrics (`metrics_v2` wide CSV) and detectors (`threshold` / `zscore` / `low_support` in `warden.yaml`). Optional `wape`/`rmse`; HighWAPE as logistics-opinionated default. Baseline long per metric. Missing metric → no-op.
- **0.1.0 (Go rewrite):** Port fatia 1 from Python to Go. Same detectors, CSV/incident contracts, and exit codes.

## License

[MIT](LICENSE)
