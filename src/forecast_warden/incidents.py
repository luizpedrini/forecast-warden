"""Incident creation, persistence (md + json), and resolution."""

from __future__ import annotations

import hashlib
import json
import re
from datetime import datetime, timezone
from pathlib import Path

from forecast_warden.models import (
    Finding,
    Hypothesis,
    Incident,
    IncidentStatus,
    Severity,
    SuggestedAction,
)

SEVERITY_RANK = {Severity.INFO: 0, Severity.WARNING: 1, Severity.CRITICAL: 2}


def actionable_findings(findings: list[Finding]) -> list[Finding]:
    """Findings that can open an incident (warning or critical)."""
    return [f for f in findings if f.severity in (Severity.WARNING, Severity.CRITICAL)]


def max_severity(findings: list[Finding]) -> Severity:
    if not findings:
        return Severity.INFO
    return max(findings, key=lambda f: SEVERITY_RANK[f.severity]).severity


def suggest_hypotheses(findings: list[Finding]) -> list[Hypothesis]:
    codes = {f.code for f in findings}
    hyps: list[Hypothesis] = []
    if "HighMAPE" in codes:
        hyps.extend([Hypothesis.DEMAND_SPIKE, Hypothesis.MODEL_STALE])
    if "BiasShift" in codes:
        hyps.extend([Hypothesis.FEATURE_BREAK, Hypothesis.CALENDAR])
    if "CoverageBreak" in codes:
        hyps.append(Hypothesis.MODEL_STALE)
    if "LowSupport" in codes:
        hyps.append(Hypothesis.DATA_DELAY)
    # unique preserve order
    seen: set[Hypothesis] = set()
    out: list[Hypothesis] = []
    for h in hyps:
        if h not in seen:
            seen.add(h)
            out.append(h)
    return out or [Hypothesis.MODEL_STALE]


def suggest_action(severity: Severity, findings: list[Finding]) -> SuggestedAction:
    codes = {f.code for f in findings}
    if severity == Severity.CRITICAL:
        return SuggestedAction.HOLD_PROMOTION
    if "BiasShift" in codes and "HighMAPE" in codes:
        return SuggestedAction.RETRAIN
    return SuggestedAction.INVESTIGATE


def make_incident_id(run_id: str, findings: list[Finding]) -> str:
    compact = run_id.replace("-", "")
    payload = "|".join(
        sorted(f"{f.code}:{f.entity_id}:{f.severity.value}" for f in findings)
    )
    short = hashlib.sha1(payload.encode()).hexdigest()[:4]
    return f"fw-{compact}-{short}"


def build_incident(run_id: str, findings: list[Finding], created_at: datetime | None = None) -> Incident | None:
    action = actionable_findings(findings)
    if not action:
        return None
    created = created_at or datetime.now(timezone.utc)
    severity = max_severity(action)
    entities = sorted({f.entity_id for f in action})
    codes = sorted({f.code for f in action})
    return Incident(
        id=make_incident_id(run_id, action),
        run_id=run_id,
        status=IncidentStatus.OPEN,
        severity=severity,
        entities=entities,
        detector_codes=codes,
        created_at=created,
        findings=findings,  # keep info findings too for context
        hypotheses=suggest_hypotheses(findings),
        suggested_action=suggest_action(severity, action),
    )


def incident_paths(incidents_dir: Path, incident: Incident) -> tuple[Path, Path]:
    short = incident.id.split("-")[-1]
    stem = f"{incident.run_id}__{short}"
    return incidents_dir / f"{stem}.md", incidents_dir / f"{stem}.json"


def write_incident(incidents_dir: Path, incident: Incident) -> tuple[Path, Path]:
    incidents_dir.mkdir(parents=True, exist_ok=True)
    md_path, json_path = incident_paths(incidents_dir, incident)
    json_path.write_text(
        incident.model_dump_json(indent=2),
        encoding="utf-8",
    )
    md_path.write_text(render_markdown(incident), encoding="utf-8")
    return md_path, json_path


def render_markdown(incident: Incident) -> str:
    entities = ", ".join(f"`{e}`" for e in incident.entities)
    codes = ", ".join(incident.detector_codes)
    hyp_lines = "\n".join(f"- `{h.value}`" for h in incident.hypotheses)
    rows = []
    for f in incident.findings:
        ev = ", ".join(f"{k}={v}" for k, v in f.evidence.items())
        rows.append(
            f"| {f.code} | {f.severity.value} | {f.entity_id} | {ev} | {f.hint} |"
        )
    table = "\n".join(rows) if rows else "| — | — | — | — | — |"
    gate = (
        "Model promotion / config change blocked while status is `open` "
        "(resolve or accept-risk first)."
        if incident.severity in (Severity.WARNING, Severity.CRITICAL)
        else "No gate."
    )
    note_block = f"\n**Resolution note:** {incident.note}\n" if incident.note else ""
    return f"""---
id: {incident.id}
run_id: "{incident.run_id}"
status: {incident.status.value}
severity: {incident.severity.value}
entities: {incident.entities}
detector_codes: {incident.detector_codes}
created_at: {incident.created_at.isoformat()}
suggested_action: {incident.suggested_action.value}
---

# Forecast Incident `{incident.id}`

## Symptom

Run `{incident.run_id}` flagged {entities} via {codes}
(severity **{incident.severity.value}**). Review evidence before promoting the model.
{note_block}
## Evidence

| detector | severity | entity | evidence | hint |
|----------|----------|--------|----------|------|
{table}

## Hypotheses (candidate)

{hyp_lines}

## Suggested action

`{incident.suggested_action.value}`

## Gate

{gate}
"""


def list_incidents(
    incidents_dir: Path,
    status: IncidentStatus | None = None,
) -> list[Incident]:
    if not incidents_dir.exists():
        return []
    incidents: list[Incident] = []
    for path in sorted(incidents_dir.glob("*.json")):
        data = json.loads(path.read_text(encoding="utf-8"))
        inc = Incident.model_validate(data)
        if status is None or inc.status == status:
            incidents.append(inc)
    return incidents


def load_incident(incidents_dir: Path, incident_id: str) -> Incident | None:
    for inc in list_incidents(incidents_dir):
        if inc.id == incident_id:
            return inc
    # also allow short suffix / stem match
    for path in incidents_dir.glob("*.json") if incidents_dir.exists() else []:
        data = json.loads(path.read_text(encoding="utf-8"))
        inc = Incident.model_validate(data)
        if incident_id in (inc.id, path.stem, path.stem.split("__")[-1]):
            return inc
    return None


def resolve_incident(
    incidents_dir: Path,
    incident_id: str,
    note: str,
    status: IncidentStatus = IncidentStatus.RESOLVED,
) -> Incident | None:
    inc = load_incident(incidents_dir, incident_id)
    if inc is None:
        return None
    updated = inc.model_copy(
        update={
            "status": status,
            "note": note,
            "resolved_at": datetime.now(timezone.utc),
        }
    )
    write_incident(incidents_dir, updated)
    return updated


FRONTMATTER_RE = re.compile(r"^---\n(.*?)\n---\n", re.DOTALL)
