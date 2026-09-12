"""Generate ~14 days of synthetic zone metrics (some intentionally sick)."""

from __future__ import annotations

import math
import random
from datetime import datetime, timedelta, timezone

from forecast_warden.models import BaselineStats, MetricRow

ZONES = ["Z1", "Z2", "Z3", "Z4", "Z5", "Z6", "Z7", "Z8"]

# Intentionally sick zones on the latest run (for demo / golden path).
SICK_PROFILES: dict[str, dict] = {
    "Z3": {"mape": 0.42, "bias": 0.22, "coverage_80": 0.55, "n_actuals": 80},  # critical MAPE + bias + cov
    "Z7": {"mape": 0.31, "bias": -0.18, "coverage_80": 0.72, "n_actuals": 60},  # warning MAPE + bias
    "Z5": {"mape": 0.12, "bias": 0.02, "coverage_80": 0.81, "n_actuals": 12},  # low support only
}


def generate_metrics(
    days: int = 14,
    end_date: datetime | None = None,
    seed: int = 42,
) -> list[MetricRow]:
    rng = random.Random(seed)
    if end_date is None:
        end_date = datetime(2026, 9, 12, 6, 0, 0, tzinfo=timezone.utc)
    rows: list[MetricRow] = []
    for d in range(days):
        day = end_date - timedelta(days=days - 1 - d)
        run_id = day.strftime("%Y-%m-%d")
        generated_at = day
        for zone in ZONES:
            if d == days - 1 and zone in SICK_PROFILES:
                p = SICK_PROFILES[zone]
                rows.append(
                    MetricRow(
                        run_id=run_id,
                        entity_type="zone",
                        entity_id=zone,
                        mape=p["mape"],
                        bias=p["bias"],
                        coverage_80=p["coverage_80"],
                        n_actuals=p["n_actuals"],
                        generated_at=generated_at,
                    )
                )
                continue
            # Healthy-ish noise; last day kept especially calm so only SICK_PROFILES fire
            mape = max(0.05, min(0.20, 0.12 + rng.gauss(0, 0.02)))
            bias_sigma = 0.015 if d == days - 1 else 0.03
            bias_cap = 0.05 if d == days - 1 else 0.10
            bias = max(-bias_cap, min(bias_cap, rng.gauss(0, bias_sigma)))
            coverage = max(0.70, min(0.95, 0.82 + rng.gauss(0, 0.03)))
            n = int(max(40, min(120, 70 + rng.gauss(0, 15))))
            rows.append(
                MetricRow(
                    run_id=run_id,
                    entity_type="zone",
                    entity_id=zone,
                    mape=round(mape, 6),
                    bias=round(bias, 6),
                    coverage_80=round(coverage, 6),
                    n_actuals=n,
                    generated_at=generated_at,
                )
            )
    return rows


def compute_baseline(metrics: list[MetricRow], window_days: int = 28) -> list[BaselineStats]:
    """Rolling mean/std from all but the last run_id (so latest can z-score)."""
    if not metrics:
        return []
    run_ids = sorted({m.run_id for m in metrics})
    history_ids = set(run_ids[:-1]) if len(run_ids) > 1 else set(run_ids)
    by_entity: dict[str, list[MetricRow]] = {}
    for m in metrics:
        if m.run_id not in history_ids:
            continue
        by_entity.setdefault(m.entity_id, []).append(m)

    out: list[BaselineStats] = []
    for entity_id, rows in sorted(by_entity.items()):
        mapes = [r.mape for r in rows]
        biases = [r.bias for r in rows]
        out.append(
            BaselineStats(
                entity_type="zone",
                entity_id=entity_id,
                mape_mean=_mean(mapes),
                mape_std=_std(mapes),
                bias_mean=_mean(biases),
                bias_std=_std(biases),
                window_days=window_days,
            )
        )
    return out


def _mean(xs: list[float]) -> float:
    return sum(xs) / len(xs) if xs else 0.0


def _std(xs: list[float]) -> float:
    if len(xs) < 2:
        return 0.01  # avoid zero-division in z-score
    m = _mean(xs)
    var = sum((x - m) ** 2 for x in xs) / (len(xs) - 1)
    return max(math.sqrt(var), 0.01)
