# forecast-warden

**Rule-based CLI for forecast pipeline ops:** synthetic metrics → detectors → Forecast Incident (markdown + JSON) → human resolve.

MIT · **Go** (single static binary) · No LLM · No UI · No real company data.

> **Language decision:** fatia 1 originally shipped in Python; ported to **Go** for a single distributable binary and easier ops/adoption in CI and on-call laptops (no venv/pip). Product contracts (CSV, detectors, incidents, exit codes) are unchanged.

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

## Detectors (MVP)

| Detector | Rule | Severity |
|----------|------|----------|
| **HighMAPE** | `mape > 0.25` & `n ≥ 30` → warning; `mape > 0.40` → critical | warning / critical |
| **BiasShift** | `\|bias\| > 0.15` **or** `\|z\| > 3` vs baseline | warning |
| **LowSupport** | `n < 30` | **info only** (alone does **not** open an incident) |
| **CoverageBreak** | `coverage_80 < 0.60` & `n ≥ 30` | warning |

An incident opens only if there is ≥1 warning/critical finding.

## Metrics CSV columns

`run_id`, `entity_type`, `entity_id`, `mape` (0–1), `bias`, `coverage_80`, `n_actuals`, `generated_at`

## CLI

```
warden init
warden check [--run-id YYYY-MM-DD] [--metrics PATH]
warden list [--status open|resolved|accepted-risk|wontfix]
warden show <id>
warden resolve <id> --note "..." [--status resolved|accepted-risk|wontfix]
```

## Non-goals (fatia 1)

- Training or serving models
- Feature store / Feast
- Required LLM narration
- Fleet / promise integration
- Web UI
- Real company data or IP

See [SPEC.md](SPEC.md) for the full fatia-1 contract.

## Develop

```bash
go test ./...
go build -o warden ./cmd/warden
```

## Changelog

- **0.1.0 (Go rewrite):** Port fatia 1 from Python to Go. Same detectors, CSV/incident contracts, and exit codes. Python package removed; use the `warden` binary.

## License

[MIT](LICENSE)
