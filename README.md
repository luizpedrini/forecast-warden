# forecast-warden

**Rule-based CLI for forecast pipeline ops:** synthetic metrics → pluggable detectors → Forecast Incident (markdown + JSON) → human resolve.

MIT · **Go** (single static binary) · Optional LLM enrichment · No UI · No real company data.

> **Language decision:** fatia 1 originally shipped in Python; ported to **Go** for a single distributable binary and easier ops/adoption in CI and on-call laptops (no venv/pip). Product contracts (CSV, incidents, exit codes) stay stable; detectors are now **pluggable via `warden.yaml`**.

## Problem

Forecast teams invest in *models* and under-invest in *cycle management*: drift, bias, broken features, calendar effects. Knowledge lives in Slack. There is no standard artifact (incident) and no human gate before promote/retrain.

## Ritual (more important than the code)

1. After the batch: `warden check`
2. If any finding ≥ `warning`: an `open` Forecast Incident is written
3. Weekly triage (15 min): humans review `open` incidents (`warden list` → `warden show <id>`)
4. **Investigate:** `warden investigate <id>` prints a rule-based playbook (attack order, questions, 15-min checklist); `--write` saves `incidents/<id>.investigate.md`
5. **Optional LLM:** `warden investigate <id> --llm` adds `## LLM synthesis` (env API key); failures fall back to rule-based
6. **Gate:** promote / change config only after critical incidents are `resolved` or `accepted-risk`
7. Close the loop with `warden resolve <id> --note "..."` (playbook includes a ready-made `--note` line)

## Quickstart (5-minute demo)

```bash
git clone https://github.com/luizpedrini/forecast-warden.git
cd forecast-warden
go build -o warden ./cmd/warden

./warden init          # config + ~14d synthetic zone metrics (some sick) + baseline
./warden check         # writes ≥1 incident under incidents/ (exit 2 if critical)
./warden list
./warden show <id>     # use id from list, e.g. fw-20260912-xxxx
./warden investigate <id>          # rule-based playbook (stdout)
./warden investigate <id> --write  # also saves incidents/<id>.investigate.md
./warden investigate <id> --llm    # optional LLM synthesis (needs API key)
./warden resolve <id> --note "reviewed; hold promotion until retune"
```

Exit codes (CI-friendly): `0` ok · `1` warning · `2` critical.

Demo sick zones (latest run): **Z3** critical WAPE + bias + unstable forecast; **Z7** warning WAPE/stability + bias; **Z5** low support (info only).

## Recommended logistics triad: WAPE, bias, stability

Opinionated defaults for demand / logistics forecast ops:

| Metric | Why |
|--------|-----|
| **WAPE** | Volume-weighted absolute % error — preferred over MAPE when volumes vary across SKUs/zones |
| **bias** | Systematic over/under-forecast vs baseline (abs + z-score) |
| **stability** | Forecast churn: mean absolute relative change of the point forecast vs the previous origin for the same target horizon (fraction ≥0; **0 = perfectly stable**). Higher = worse. The warden does **not** compute it from y/yhat — the adopter’s batch writes the column (same as wape/bias). Synth emits a plausible `stability` column |

MAPE / RMSE / coverage remain available as **optional** detector examples in config (not defaults).

## Pluggable metrics & detectors

Metrics CSV is **wide `metrics_v2`**: stable identity columns plus optional float metrics. Unknown / non-float columns are ignored. Detectors that reference a **missing** metric produce **no finding** (no crash) — so old CSVs without `wape`/`stability` keep working (those detectors no-op).

| Stable columns | Optional metrics (examples) |
|----------------|----------------------------|
| `run_id`, `entity_type`, `entity_id`, `n_actuals`, `generated_at` | **`wape`**, **`bias`**, **`stability`**, `mape`, `rmse`, `coverage_80`, … |

Configure detectors in `warden.yaml` (`schema_version: 2`):

| Type | Role |
|------|------|
| `threshold` | Alert when metric is `above` or `below` warning/critical |
| `zscore` | Alert on `\|value\| > abs_warning` **or** `\|z\| > z_warning` vs baseline |
| `low_support` | `n_actuals < min_support` (default severity `info`) |

### Default detectors

| Detector | Rule | Severity |
|----------|------|----------|
| **HighWAPE** | `wape > 0.30` / `> 0.45` & `n ≥ 30` | warning / critical |
| **BiasShift** | `\|bias\| > 0.15` **or** `\|z\| > 3` vs baseline | warning |
| **UnstableForecast** | `stability > 0.15` / `> 0.30` & `n ≥ 30` (`direction: above`) | warning / critical |
| **LowSupport** | `n < 30` | **info only** (alone does **not** open an incident) |

### Optional examples (not defaults)

Add these in `warden.yaml` if you still track them:

| Detector | Rule |
|----------|------|
| **HighMAPE** | `mape > 0.25` / `> 0.40` |
| **HighRMSE** | `rmse > 10` / `> 20` |
| **CoverageBreak** | `coverage_80 < 0.60` (`direction: below`) |

Baseline is **long** (`entity_id`, `metric`, `mean`, `std`); includes stability mean/std when present. Legacy wide `mape_mean`/`bias_mean` files still load. UnstableForecast is threshold-only (baseline optional).

An incident opens only if there is ≥1 warning/critical finding. Re-running `check` does **not** clobber terminal statuses (`resolved` / `accepted-risk` / `wontfix`).

## CLI

```
warden init
warden check [--run-id YYYY-MM-DD] [--metrics PATH]
warden list [--status open|resolved|accepted-risk|wontfix]
warden show <id>
warden investigate <id> [--write] [--llm] [--provider openai|anthropic]
warden resolve <id> --note "..." [--status resolved|accepted-risk|wontfix]
```

`investigate` always builds the **rule-based** playbook first (templates per detector + combo hints). Attack order: UnstableForecast → BiasShift → HighWAPE → other thresholds → LowSupport last. No warehouse/SQL — only generic “look outside warden” guidance.

### Optional `--llm` enrichment

```bash
export OPENAI_API_KEY=sk-...          # or ANTHROPIC_API_KEY
# optional:
export WARDEN_LLM_PROVIDER=openai     # or anthropic
export WARDEN_LLM_MODEL=gpt-4o-mini   # provider default if unset

./warden investigate <id> --llm
./warden investigate <id> --llm --provider anthropic --write
```

- Adds a `## LLM synthesis` section (short paragraph + ranked hypotheses + optional refined `--note`).
- Prompt context is **only** the incident JSON + rule-based playbook; the system prompt forbids inventing SQL, company systems, or ungrounded root causes.
- Keys from **environment only** (never CLI flags). Do not send proprietary warehouse dumps — only the metrics already on the incident.
- On any LLM error (missing key, network, bad JSON): **stderr warning** + rule-based output; exit `0` if investigate itself succeeded (CI-safe).
- `--llm` never auto-resolves or changes detector thresholds.

## Non-goals

- Training or serving models / computing metrics from y/yhat raw (including stability)
- Feature store / Feast / long-format required
- Required LLM narration (optional `--llm` only)
- Fleet / promise integration
- Web UI
- Real company data or IP

See [SPEC.md](SPEC.md) for the full contract (fatia 1 + pluggable metrics + stability + investigate).

## Develop

```bash
go test ./...
go build -o warden ./cmd/warden
```

## Changelog

- **0.5.0:** `warden investigate --llm` — optional LLM enrichment (`## LLM synthesis`) via OpenAI/Anthropic (stdlib HTTP, env keys). Rule-based playbook remains source of truth; LLM failure → stderr warning + rule-based fallback (exit 0).
- **0.4.0:** `warden investigate <id>` — rule-based investigation playbook (attack order, per-finding questions, 15-min checklist, suggested resolve decision, ready-made `--note`). `--write` → `incidents/<id>.investigate.md`. Ritual: check → list → show → **investigate** → resolve.
- **0.3.0:** Opinionated logistics triad defaults: **WAPE + bias + stability** (UnstableForecast). HighMAPE/HighRMSE/CoverageBreak demoted to optional examples. Synth emits `stability`; missing column → UnstableForecast no-op.
- **0.2.0:** Pluggable metrics (`metrics_v2` wide CSV) and detectors (`threshold` / `zscore` / `low_support` in `warden.yaml`). Optional `wape`/`rmse`; HighWAPE as logistics-opinionated default. Baseline long per metric. Missing metric → no-op.
- **0.1.0 (Go rewrite):** Port fatia 1 from Python to Go. Same detectors, CSV/incident contracts, and exit codes.

## License

[MIT](LICENSE)
