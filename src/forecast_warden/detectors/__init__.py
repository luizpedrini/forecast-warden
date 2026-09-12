"""Rule-based detectors."""

from __future__ import annotations

from forecast_warden.config import DetectorThresholds
from forecast_warden.detectors.base import Detector
from forecast_warden.detectors.bias_shift import BiasShiftDetector
from forecast_warden.detectors.coverage_break import CoverageBreakDetector
from forecast_warden.detectors.high_mape import HighMAPEDetector
from forecast_warden.detectors.low_support import LowSupportDetector
from forecast_warden.models import BaselineStats, Finding, MetricRow


def default_detectors(thresholds: DetectorThresholds | None = None) -> list[Detector]:
    t = thresholds or DetectorThresholds()
    return [
        HighMAPEDetector(t),
        BiasShiftDetector(t),
        LowSupportDetector(t),
        CoverageBreakDetector(t),
    ]


def run_all(
    metrics: list[MetricRow],
    baseline: dict[str, BaselineStats] | None = None,
    thresholds: DetectorThresholds | None = None,
) -> list[Finding]:
    findings: list[Finding] = []
    for detector in default_detectors(thresholds):
        findings.extend(detector.detect(metrics, baseline or {}))
    return findings


__all__ = [
    "Detector",
    "BiasShiftDetector",
    "CoverageBreakDetector",
    "HighMAPEDetector",
    "LowSupportDetector",
    "default_detectors",
    "run_all",
]
