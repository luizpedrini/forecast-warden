"""HighMAPE detector."""

from __future__ import annotations

from forecast_warden.detectors.base import Detector
from forecast_warden.models import BaselineStats, Finding, MetricRow, Severity


class HighMAPEDetector(Detector):
    code = "HighMAPE"

    def detect(
        self,
        metrics: list[MetricRow],
        baseline: dict[str, BaselineStats],
    ) -> list[Finding]:
        del baseline  # unused
        t = self.thresholds
        findings: list[Finding] = []
        for row in metrics:
            if row.n_actuals < t.min_support:
                continue
            if row.mape > t.high_mape_critical:
                severity = Severity.CRITICAL
            elif row.mape > t.high_mape_warning:
                severity = Severity.WARNING
            else:
                continue
            findings.append(
                Finding(
                    code=self.code,
                    severity=severity,
                    entity_id=row.entity_id,
                    evidence={
                        "mape": row.mape,
                        "n_actuals": row.n_actuals,
                        "warning_threshold": t.high_mape_warning,
                        "critical_threshold": t.high_mape_critical,
                    },
                    hint="MAPE elevated vs threshold; check demand spike or model stale.",
                )
            )
        return findings
