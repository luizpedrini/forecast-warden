"""Typer CLI entrypoint: warden init|check|list|show|resolve."""

from __future__ import annotations

from pathlib import Path
from typing import Optional

import typer
from rich.console import Console
from rich.table import Table

from forecast_warden import __version__
from forecast_warden.config import load_config, write_default_config
from forecast_warden.detectors import run_all
from forecast_warden.incidents import (
    actionable_findings,
    build_incident,
    list_incidents,
    load_incident,
    max_severity,
    resolve_incident,
    write_incident,
)
from forecast_warden.io import read_baseline, read_metrics, write_baseline, write_metrics
from forecast_warden.models import IncidentStatus, Severity
from forecast_warden.synthetic import compute_baseline, generate_metrics

app = typer.Typer(
    name="warden",
    help="Rule-based forecast pipeline ops: metrics → detectors → Forecast Incident.",
    no_args_is_help=True,
)
console = Console()


def _exit_for_severity(severity: Severity | None) -> None:
    if severity == Severity.CRITICAL:
        raise typer.Exit(code=2)
    if severity == Severity.WARNING:
        raise typer.Exit(code=1)
    raise typer.Exit(code=0)


@app.callback()
def main(
    version: bool = typer.Option(False, "--version", help="Show version and exit."),
) -> None:
    if version:
        console.print(f"forecast-warden {__version__}")
        raise typer.Exit()


@app.command("init")
def init_cmd(
    force: bool = typer.Option(False, "--force", help="Overwrite existing data/config."),
    days: int = typer.Option(14, "--days", help="Days of synthetic metrics."),
) -> None:
    """Create config + ~14d synthetic zone metrics (some sick) + baseline."""
    cfg_path = Path("warden.yaml")
    if cfg_path.exists() and not force:
        console.print("[yellow]warden.yaml already exists (use --force to overwrite).[/yellow]")
    else:
        write_default_config(cfg_path)
        console.print(f"[green]Wrote[/green] {cfg_path}")

    cfg = load_config(cfg_path)
    metrics_path = cfg.metrics_path()
    baseline_path = cfg.baseline_path()

    if metrics_path.exists() and not force:
        console.print(f"[yellow]{metrics_path} exists (use --force).[/yellow]")
    else:
        rows = generate_metrics(days=days)
        write_metrics(metrics_path, rows)
        console.print(f"[green]Wrote[/green] {metrics_path} ({len(rows)} rows, {days} days)")

    metrics = read_metrics(metrics_path)
    baseline_rows = compute_baseline(metrics)
    write_baseline(baseline_path, baseline_rows)
    console.print(f"[green]Wrote[/green] {baseline_path} ({len(baseline_rows)} entities)")

    cfg.incidents_dir.mkdir(parents=True, exist_ok=True)
    console.print(f"[green]Ready[/green] incidents dir: {cfg.incidents_dir}/")
    console.print("Next: [bold]warden check[/bold]")


@app.command("check")
def check_cmd(
    run_id: Optional[str] = typer.Option(
        None, "--run-id", help="Check a specific run_id (default: latest in CSV)."
    ),
    metrics: Optional[Path] = typer.Option(None, "--metrics", help="Path to metrics CSV."),
    config: Path = typer.Option(Path("warden.yaml"), "--config", help="Config path."),
) -> None:
    """Run detectors on metrics; write incident(s) if warning/critical."""
    cfg = load_config(config if config.exists() else None)
    metrics_path = metrics or cfg.metrics_path()
    if not metrics_path.exists():
        console.print(f"[red]Metrics not found:[/red] {metrics_path}. Run [bold]warden init[/bold].")
        raise typer.Exit(code=2)

    all_rows = read_metrics(metrics_path)
    if not all_rows:
        console.print("[red]Metrics CSV is empty.[/red]")
        raise typer.Exit(code=2)

    available = sorted({r.run_id for r in all_rows})
    target = run_id or available[-1]
    rows = [r for r in all_rows if r.run_id == target]
    if not rows:
        console.print(f"[red]No rows for run_id={target}[/red]. Available: {available}")
        raise typer.Exit(code=2)

    baseline = read_baseline(cfg.baseline_path())
    findings = run_all(rows, baseline, cfg.thresholds)
    action = actionable_findings(findings)

    table = Table(title=f"Findings for run {target}")
    table.add_column("code")
    table.add_column("severity")
    table.add_column("entity")
    table.add_column("hint")
    for f in findings:
        table.add_row(f.code, f.severity.value, f.entity_id, f.hint)
    if findings:
        console.print(table)
    else:
        console.print("[green]No findings.[/green]")

    incident = build_incident(target, findings)
    if incident is None:
        console.print("No incident opened (info-only or clean).")
        _exit_for_severity(None)

    md_path, json_path = write_incident(cfg.incidents_dir, incident)
    console.print(
        f"[bold]Incident[/bold] {incident.id} "
        f"({incident.severity.value}) → {md_path.name} + {json_path.name}"
    )
    _exit_for_severity(incident.severity)


@app.command("list")
def list_cmd(
    status: Optional[str] = typer.Option(
        None, "--status", help="Filter: open|resolved|accepted-risk|wontfix"
    ),
    config: Path = typer.Option(Path("warden.yaml"), "--config"),
) -> None:
    """List incidents (optionally by status)."""
    cfg = load_config(config if config.exists() else None)
    st = IncidentStatus(status) if status else None
    incidents = list_incidents(cfg.incidents_dir, st)
    if not incidents:
        console.print("No incidents.")
        raise typer.Exit(code=0)

    table = Table(title="Incidents")
    table.add_column("id", no_wrap=True, overflow="fold")
    table.add_column("run_id")
    table.add_column("status")
    table.add_column("severity")
    table.add_column("entities")
    table.add_column("detectors")
    for inc in incidents:
        table.add_row(
            inc.id,
            inc.run_id,
            inc.status.value,
            inc.severity.value,
            ",".join(inc.entities),
            ",".join(inc.detector_codes),
        )
    console.print(table)


@app.command("show")
def show_cmd(
    incident_id: str = typer.Argument(..., help="Incident id (e.g. fw-20260912-a1b2)"),
    config: Path = typer.Option(Path("warden.yaml"), "--config"),
) -> None:
    """Show one incident (markdown body if present)."""
    cfg = load_config(config if config.exists() else None)
    inc = load_incident(cfg.incidents_dir, incident_id)
    if inc is None:
        console.print(f"[red]Incident not found:[/red] {incident_id}")
        raise typer.Exit(code=2)

    # Prefer markdown companion
    for md in cfg.incidents_dir.glob("*.md"):
        if incident_id in md.read_text(encoding="utf-8")[:200] or incident_id in md.stem:
            # check id in frontmatter more carefully
            text = md.read_text(encoding="utf-8")
            if f"id: {inc.id}" in text or f"id: {incident_id}" in text:
                console.print(text)
                return
    console.print_json(inc.model_dump_json())


@app.command("resolve")
def resolve_cmd(
    incident_id: str = typer.Argument(...),
    note: str = typer.Option(..., "--note", help="Resolution note (required)."),
    status: str = typer.Option(
        "resolved",
        "--status",
        help="resolved | accepted-risk | wontfix",
    ),
    config: Path = typer.Option(Path("warden.yaml"), "--config"),
) -> None:
    """Mark an incident resolved / accepted-risk / wontfix."""
    cfg = load_config(config if config.exists() else None)
    try:
        st = IncidentStatus(status)
    except ValueError:
        console.print(f"[red]Invalid status:[/red] {status}")
        raise typer.Exit(code=2)
    if st == IncidentStatus.OPEN:
        console.print("[red]Cannot resolve to open.[/red]")
        raise typer.Exit(code=2)

    updated = resolve_incident(cfg.incidents_dir, incident_id, note=note, status=st)
    if updated is None:
        console.print(f"[red]Incident not found:[/red] {incident_id}")
        raise typer.Exit(code=2)
    console.print(f"[green]Updated[/green] {updated.id} → {updated.status.value}")


if __name__ == "__main__":
    app()
