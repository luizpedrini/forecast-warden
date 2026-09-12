"""CSV I/O for metrics and baseline stats."""

from __future__ import annotations

import csv
from datetime import datetime
from pathlib import Path

from forecast_warden.models import BaselineStats, MetricRow

METRICS_FIELDS = [
    "run_id",
    "entity_type",
    "entity_id",
    "mape",
    "bias",
    "coverage_80",
    "n_actuals",
    "generated_at",
]

BASELINE_FIELDS = [
    "entity_type",
    "entity_id",
    "mape_mean",
    "mape_std",
    "bias_mean",
    "bias_std",
    "window_days",
]


def read_metrics(path: Path) -> list[MetricRow]:
    rows: list[MetricRow] = []
    with path.open(newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        for raw in reader:
            rows.append(
                MetricRow(
                    run_id=raw["run_id"],
                    entity_type=raw.get("entity_type", "zone"),
                    entity_id=raw["entity_id"],
                    mape=float(raw["mape"]),
                    bias=float(raw["bias"]),
                    coverage_80=float(raw["coverage_80"]),
                    n_actuals=int(raw["n_actuals"]),
                    generated_at=datetime.fromisoformat(raw["generated_at"]),
                )
            )
    return rows


def write_metrics(path: Path, rows: list[MetricRow]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=METRICS_FIELDS)
        writer.writeheader()
        for row in rows:
            writer.writerow(
                {
                    "run_id": row.run_id,
                    "entity_type": row.entity_type,
                    "entity_id": row.entity_id,
                    "mape": f"{row.mape:.6f}",
                    "bias": f"{row.bias:.6f}",
                    "coverage_80": f"{row.coverage_80:.6f}",
                    "n_actuals": row.n_actuals,
                    "generated_at": row.generated_at.isoformat(),
                }
            )


def read_baseline(path: Path) -> dict[str, BaselineStats]:
    if not path.exists():
        return {}
    out: dict[str, BaselineStats] = {}
    with path.open(newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        for raw in reader:
            stats = BaselineStats(
                entity_type=raw.get("entity_type", "zone"),
                entity_id=raw["entity_id"],
                mape_mean=float(raw["mape_mean"]),
                mape_std=float(raw["mape_std"]),
                bias_mean=float(raw["bias_mean"]),
                bias_std=float(raw["bias_std"]),
                window_days=int(raw.get("window_days", 28)),
            )
            out[stats.entity_id] = stats
    return out


def write_baseline(path: Path, rows: list[BaselineStats]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=BASELINE_FIELDS)
        writer.writeheader()
        for row in rows:
            writer.writerow(
                {
                    "entity_type": row.entity_type,
                    "entity_id": row.entity_id,
                    "mape_mean": f"{row.mape_mean:.6f}",
                    "mape_std": f"{row.mape_std:.6f}",
                    "bias_mean": f"{row.bias_mean:.6f}",
                    "bias_std": f"{row.bias_std:.6f}",
                    "window_days": row.window_days,
                }
            )
