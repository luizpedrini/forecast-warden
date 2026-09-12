"""LowSupport detector — info only; never opens an incident alone."""

from __future__ import annotations

from forecast_warden.detectors.base import Detector
from forecast_warden.models import BaselineStats, Finding, MetricRow, Severity


class LowSupportDetector(Detector):
    code = "LowSupport"

    def detect(
        self,
        metrics: list[MetricRow],
        baseline: dict[str, BaselineStats],
    ) -> list[Finding]:
        del baseline
        t = self.thresholds
        findings: list[Finding] = []
        for row in metrics:
            if row.n_actuals >= t.min_support:
                continue
            findings.append(
                Finding(
                    code=self.code,
                    severity=Severity.INFO,
                    entity_id=row.entity_id,
                    evidence={
                        "n_actuals": row.n_actuals,
                        "min_support": t.min_support,
                    },
                    hint="Sample size too small for reliable MAPE/coverage alerts.",
                )
            )
        return findings
