"""CLI smoke tests via Typer CliRunner."""

from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from forecast_warden.cli.app import app

runner = CliRunner()


def test_init_check_list_show_resolve(tmp_path: Path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    result = runner.invoke(app, ["init"])
    assert result.exit_code == 0, result.output
    assert (tmp_path / "warden.yaml").exists()
    assert (tmp_path / "data" / "run_metrics.csv").exists()
    assert (tmp_path / "data" / "baseline_stats.csv").exists()

    result = runner.invoke(app, ["check"])
    # critical expected from Z3 → exit 2
    assert result.exit_code == 2, result.output
    assert "Incident" in result.output
    incidents = list((tmp_path / "incidents").glob("*.json"))
    assert len(incidents) >= 1
    incident_id = json.loads(incidents[0].read_text(encoding="utf-8"))["id"]

    result = runner.invoke(app, ["list"])
    assert result.exit_code == 0
    assert "fw-" in result.output

    result = runner.invoke(app, ["show", incident_id])
    assert result.exit_code == 0, result.output
    assert incident_id in result.output

    result = runner.invoke(app, ["resolve", incident_id, "--note", "looked fine after review"])
    assert result.exit_code == 0
    assert "resolved" in result.output

    result = runner.invoke(app, ["list", "--status", "resolved"])
    assert result.exit_code == 0
    assert "resolved" in result.output
