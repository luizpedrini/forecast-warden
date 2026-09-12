"""Unit tests for the four MVP detectors."""

from __future__ import annotations

from datetime import datetime, timezone

from forecast_warden.config import DetectorThresholds
from forecast_warden.detectors import (
    BiasShiftDetector,
    CoverageBreakDetector,
    HighMAPEDetector,
    LowSupportDetector,
)
from forecast_warden.models import BaselineStats, MetricRow, Severity


def _row(**kwargs) -> MetricRow:
    base = dict(
        run_id="2026-09-12",
        entity_type="zone",
        entity_id="Z1",
        mape=0.10,
        bias=0.01,
        coverage_80=0.85,
        n_actuals=50,
        generated_at=datetime(2026, 9, 12, tzinfo=timezone.utc),
    )
    base.update(kwargs)
    return MetricRow(**base)


def test_high_mape_warning():
    det = HighMAPEDetector()
    findings = det.detect([_row(mape=0.30, n_actuals=40)], {})
    assert len(findings) == 1
    assert findings[0].severity == Severity.WARNING
    assert findings[0].code == "HighMAPE"


def test_high_mape_critical():
    det = HighMAPEDetector()
    findings = det.detect([_row(mape=0.45, n_actuals=40)], {})
    assert len(findings) == 1
    assert findings[0].severity == Severity.CRITICAL


def test_high_mape_skips_low_support():
    det = HighMAPEDetector()
    findings = det.detect([_row(mape=0.50, n_actuals=10)], {})
    assert findings == []


def test_high_mape_healthy():
    det = HighMAPEDetector()
    findings = det.detect([_row(mape=0.20, n_actuals=40)], {})
    assert findings == []


def test_bias_shift_abs():
    det = BiasShiftDetector()
    findings = det.detect([_row(bias=0.20)], {})
    assert len(findings) == 1
    assert findings[0].severity == Severity.WARNING
    assert findings[0].code == "BiasShift"


def test_bias_shift_zscore():
    det = BiasShiftDetector()
    baseline = {
        "Z1": BaselineStats(
            entity_id="Z1",
            mape_mean=0.12,
            mape_std=0.02,
            bias_mean=0.0,
            bias_std=0.02,
        )
    }
    # |bias|=0.10 < 0.15 but z = 0.10/0.02 = 5 > 3
    findings = det.detect([_row(bias=0.10)], baseline)
    assert len(findings) == 1
    assert findings[0].evidence["z_score"] == 5.0


def test_bias_shift_healthy():
    det = BiasShiftDetector()
    findings = det.detect([_row(bias=0.05)], {})
    assert findings == []


def test_low_support_info_only():
    det = LowSupportDetector()
    findings = det.detect([_row(n_actuals=12)], {})
    assert len(findings) == 1
    assert findings[0].severity == Severity.INFO
    assert findings[0].code == "LowSupport"


def test_low_support_ok():
    det = LowSupportDetector()
    findings = det.detect([_row(n_actuals=30)], {})
    assert findings == []


def test_coverage_break_warning():
    det = CoverageBreakDetector()
    findings = det.detect([_row(coverage_80=0.50, n_actuals=40)], {})
    assert len(findings) == 1
    assert findings[0].severity == Severity.WARNING
    assert findings[0].code == "CoverageBreak"


def test_coverage_break_skips_low_support():
    det = CoverageBreakDetector()
    findings = det.detect([_row(coverage_80=0.40, n_actuals=5)], {})
    assert findings == []


def test_coverage_healthy():
    det = CoverageBreakDetector()
    findings = det.detect([_row(coverage_80=0.70, n_actuals=40)], {})
    assert findings == []


def test_custom_thresholds():
    t = DetectorThresholds(high_mape_warning=0.50, high_mape_critical=0.80)
    det = HighMAPEDetector(t)
    assert det.detect([_row(mape=0.30, n_actuals=40)], {}) == []
