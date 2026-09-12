"""Default thresholds and project config."""

from __future__ import annotations

from pathlib import Path

from pydantic import BaseModel, Field


class DetectorThresholds(BaseModel):
    high_mape_warning: float = 0.25
    high_mape_critical: float = 0.40
    min_support: int = 30
    bias_abs_warning: float = 0.15
    bias_z_warning: float = 3.0
    coverage_warning: float = 0.60


class WardenConfig(BaseModel):
    data_dir: Path = Path("data")
    incidents_dir: Path = Path("incidents")
    metrics_filename: str = "run_metrics.csv"
    baseline_filename: str = "baseline_stats.csv"
    thresholds: DetectorThresholds = Field(default_factory=DetectorThresholds)

    def metrics_path(self) -> Path:
        return self.data_dir / self.metrics_filename

    def baseline_path(self) -> Path:
        return self.data_dir / self.baseline_filename


DEFAULT_CONFIG_YAML = """\
# forecast-warden configuration
data_dir: data
incidents_dir: incidents
metrics_filename: run_metrics.csv
baseline_filename: baseline_stats.csv

thresholds:
  high_mape_warning: 0.25
  high_mape_critical: 0.40
  min_support: 30
  bias_abs_warning: 0.15
  bias_z_warning: 3.0
  coverage_warning: 0.60
"""


def load_config(path: Path | None = None) -> WardenConfig:
    """Load config from YAML-ish key:value file, or return defaults."""
    cfg_path = path or Path("warden.yaml")
    if not cfg_path.exists():
        return WardenConfig()

    # Minimal YAML subset parser (no PyYAML dependency): nested via indent.
    raw: dict = {}
    stack: list[tuple[int, dict]] = [(0, raw)]
    for line in cfg_path.read_text(encoding="utf-8").splitlines():
        stripped = line.split("#", 1)[0].rstrip()
        if not stripped.strip():
            continue
        indent = len(stripped) - len(stripped.lstrip())
        key, _, val = stripped.lstrip().partition(":")
        key = key.strip()
        val = val.strip()
        while stack and indent < stack[-1][0]:
            stack.pop()
        parent = stack[-1][1]
        if val == "":
            child: dict = {}
            parent[key] = child
            stack.append((indent + 2, child))
        else:
            parent[key] = _coerce(val)
    return WardenConfig.model_validate(raw)


def _coerce(val: str):
    if val.lower() in ("true", "false"):
        return val.lower() == "true"
    try:
        if "." in val:
            return float(val)
        return int(val)
    except ValueError:
        return val


def write_default_config(path: Path) -> None:
    path.write_text(DEFAULT_CONFIG_YAML, encoding="utf-8")
