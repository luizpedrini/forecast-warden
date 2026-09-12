"""Pydantic models for metrics, findings, and incidents."""

from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Any

from pydantic import BaseModel, Field


class Severity(str, Enum):
    INFO = "info"
    WARNING = "warning"
    CRITICAL = "critical"


class IncidentStatus(str, Enum):
    OPEN = "open"
    RESOLVED = "resolved"
    ACCEPTED_RISK = "accepted-risk"
    WONTFIX = "wontfix"


class SuggestedAction(str, Enum):
    INVESTIGATE = "investigate"
    HOLD_PROMOTION = "hold_promotion"
    RETUNE_THRESHOLD = "retune_threshold"
    RETRAIN = "retrain"
    ACCEPTED_RISK = "accepted_risk"


class Hypothesis(str, Enum):
    DEMAND_SPIKE = "demand_spike"
    FEATURE_BREAK = "feature_break"
    CALENDAR = "calendar"
    MODEL_STALE = "model_stale"
    DATA_DELAY = "data_delay"


class MetricRow(BaseModel):
    run_id: str
    entity_type: str = "zone"
    entity_id: str
    mape: float = Field(ge=0.0, description="MAPE in 0–1 fraction")
    bias: float
    coverage_80: float = Field(ge=0.0, le=1.0)
    n_actuals: int = Field(ge=0)
    generated_at: datetime


class BaselineStats(BaseModel):
    entity_type: str = "zone"
    entity_id: str
    mape_mean: float
    mape_std: float
    bias_mean: float
    bias_std: float
    window_days: int = 28


class Finding(BaseModel):
    code: str
    severity: Severity
    entity_id: str
    evidence: dict[str, Any] = Field(default_factory=dict)
    hint: str = ""


class Incident(BaseModel):
    id: str
    run_id: str
    status: IncidentStatus = IncidentStatus.OPEN
    severity: Severity
    entities: list[str]
    detector_codes: list[str]
    created_at: datetime
    findings: list[Finding]
    hypotheses: list[Hypothesis] = Field(default_factory=list)
    suggested_action: SuggestedAction = SuggestedAction.INVESTIGATE
    note: str | None = None
    resolved_at: datetime | None = None
