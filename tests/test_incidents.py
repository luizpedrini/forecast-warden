"""Golden incident + LowSupport-alone must not open incident."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

from forecast_warden.detectors import run_all
from forecast_warden.incidents import actionable_findings, build_incident, write_incident
from forecast_warden.models import BaselineStats, Finding, MetricRow, Severity
from forecast_warden.synthetic import SICK_PROFILES, compute_baseline, generate_metrics


def test_low_support_alone_does_not_open_incident():
    row = MetricRow(
        run_id="2026-09-12",
        entity_id="Z5",
        mape=0.12,
        bias=0.02,
        coverage_80=0.81,
        n_actuals=12,
        generated_at=datetime(2026, 9, 12, tzinfo=timezone.utc),
    )
    findings = run_all([row], {})
    assert any(f.code == "LowSupport" for f in findings)
    assert actionable_findings(findings) == []
    assert build_incident("2026-09-12", findings) is None


def test_golden_incident_from_synthetic_latest(tmp_path: Path):
    metrics = generate_metrics(days=14, seed=42)
    baseline_rows = compute_baseline(metrics)
    baseline = {b.entity_id: b for b in baseline_rows}
    latest = sorted({m.run_id for m in metrics})[-1]
    rows = [m for m in metrics if m.run_id == latest]
    findings = run_all(rows, baseline)
    action = actionable_findings(findings)
    assert action, "expected warning/critical findings on sick zones"
    assert any(f.entity_id == "Z3" for f in action)
    assert any(f.code == "HighMAPE" and f.severity == Severity.CRITICAL for f in action)

    incident = build_incident(latest, findings, created_at=datetime(2026, 9, 12, tzinfo=timezone.utc))
    assert incident is not None
    assert incident.severity == Severity.CRITICAL
    assert "Z3" in incident.entities
    assert "HighMAPE" in incident.detector_codes

    md_path, json_path = write_incident(tmp_path, incident)
    assert md_path.exists() and json_path.exists()
    data = json.loads(json_path.read_text(encoding="utf-8"))
    assert data["id"] == incident.id
    assert data["status"] == "open"
    assert data["run_id"] == latest
    text = md_path.read_text(encoding="utf-8")
    assert incident.id in text
    assert "Forecast Incident" in text
    assert "Gate" in text


def test_sick_profiles_documented():
    assert SICK_PROFILES["Z3"]["mape"] > 0.40
    assert abs(SICK_PROFILES["Z7"]["bias"]) > 0.15
    assert SICK_PROFILES["Z5"]["n_actuals"] < 30
