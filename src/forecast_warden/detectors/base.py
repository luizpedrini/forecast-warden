"""Detector protocol / base class."""

from __future__ import annotations

from abc import ABC, abstractmethod

from forecast_warden.config import DetectorThresholds
from forecast_warden.models import BaselineStats, Finding, MetricRow


class Detector(ABC):
    code: str

    def __init__(self, thresholds: DetectorThresholds | None = None) -> None:
        self.thresholds = thresholds or DetectorThresholds()

    @abstractmethod
    def detect(
        self,
        metrics: list[MetricRow],
        baseline: dict[str, BaselineStats],
    ) -> list[Finding]:
        raise NotImplementedError
