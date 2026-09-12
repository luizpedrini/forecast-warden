"""CoverageBreak detector."""

from __future__ import annotations

from forecast_warden.detectors.base import Detector
from forecast_warden.models import BaselineStats, Finding, MetricRow, Severity


class CoverageBreakDetector(Detector):
    code = "CoverageBreak"

    def detect(
        self,
        metrics: list[MetricRow],
        baseline: dict[str, BaselineStats],
    ) -> list[Finding]:
        del baseline
        t = self.thresholds
        findings: list[Finding] = []
        for row in metrics:
            if row.n_actuals < t.min_support:
                continue
            if row.coverage_80 >= t.coverage_warning:
                continue
            findings.append(
                Finding(
                    code=self.code,
                    severity=Severity.WARNING,
                    entity_id=row.entity_id,
                    evidence={
                        "coverage_80": row.coverage_80,
                        "n_actuals": row.n_actuals,
                        "threshold": t.coverage_warning,
                    },
                    hint="Prediction intervals undercovering; check variance / calibration.",
                )
            )
        return findings
