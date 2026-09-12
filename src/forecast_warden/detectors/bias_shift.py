"""BiasShift detector."""

from __future__ import annotations

from forecast_warden.detectors.base import Detector
from forecast_warden.models import BaselineStats, Finding, MetricRow, Severity


class BiasShiftDetector(Detector):
    code = "BiasShift"

    def detect(
        self,
        metrics: list[MetricRow],
        baseline: dict[str, BaselineStats],
    ) -> list[Finding]:
        t = self.thresholds
        findings: list[Finding] = []
        for row in metrics:
            abs_bias = abs(row.bias)
            z: float | None = None
            stats = baseline.get(row.entity_id)
            if stats is not None and stats.bias_std > 0:
                z = abs((row.bias - stats.bias_mean) / stats.bias_std)

            triggered_abs = abs_bias > t.bias_abs_warning
            triggered_z = z is not None and z > t.bias_z_warning
            if not (triggered_abs or triggered_z):
                continue

            findings.append(
                Finding(
                    code=self.code,
                    severity=Severity.WARNING,
                    entity_id=row.entity_id,
                    evidence={
                        "bias": row.bias,
                        "abs_bias": abs_bias,
                        "z_score": z,
                        "abs_threshold": t.bias_abs_warning,
                        "z_threshold": t.bias_z_warning,
                        "baseline_mean": stats.bias_mean if stats else None,
                        "baseline_std": stats.bias_std if stats else None,
                    },
                    hint="Systematic over/under-forecast; check feature break or calendar.",
                )
            )
        return findings
