package csvio

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/models"
)

// Stable identity columns for metrics_v2 (wide).
var StableMetricFields = []string{
	"run_id", "entity_type", "entity_id", "n_actuals", "generated_at",
}

// PreferredMetricOrder is the preferred column order for well-known metrics.
var PreferredMetricOrder = []string{"mape", "bias", "coverage_80", "wape", "rmse"}

var stableSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(StableMetricFields))
	for _, k := range StableMetricFields {
		m[k] = struct{}{}
	}
	return m
}()

func ReadMetrics(path string) ([]models.MetricRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := indexMap(header)
	for _, req := range []string{"run_id", "entity_id", "n_actuals", "generated_at"} {
		if _, ok := idx[req]; !ok {
			return nil, fmt.Errorf("metrics CSV missing required column %q", req)
		}
	}

	// Optional float metric columns = anything not stable.
	var metricCols []string
	for _, h := range header {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, isStable := stableSet[h]; isStable {
			continue
		}
		metricCols = append(metricCols, h)
	}

	var rows []models.MetricRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(k string) string {
			i, ok := idx[k]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		n, err := strconv.Atoi(get("n_actuals"))
		if err != nil {
			return nil, fmt.Errorf("n_actuals: %w", err)
		}
		ts, err := parseTime(get("generated_at"))
		if err != nil {
			return nil, fmt.Errorf("generated_at: %w", err)
		}
		et := get("entity_type")
		if et == "" {
			et = "zone"
		}
		metrics := map[string]float64{}
		for _, col := range metricCols {
			raw := get(col)
			if raw == "" {
				continue // empty = absent
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				// Unknown / non-float columns ignored.
				continue
			}
			metrics[col] = v
		}
		rows = append(rows, models.MetricRow{
			RunID:       get("run_id"),
			EntityType:  et,
			EntityID:    get("entity_id"),
			NActuals:    n,
			GeneratedAt: ts,
			Metrics:     metrics,
		})
	}
	return rows, nil
}

func WriteMetrics(path string, rows []models.MetricRow) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	metricKeys := collectMetricKeys(rows)
	header := append([]string{}, StableMetricFields[:3]...) // run_id, entity_type, entity_id
	// Insert preferred metrics after identity, before n_actuals for readable wide CSV.
	// Spec stable cols: run_id, entity_type, entity_id, n_actuals, generated_at
	// with optional metrics anywhere; we write: identity + metrics + n_actuals + generated_at
	// Actually for readability matching v1: run_id, entity_type, entity_id, mape, bias, ..., n_actuals, generated_at
	header = []string{"run_id", "entity_type", "entity_id"}
	header = append(header, metricKeys...)
	header = append(header, "n_actuals", "generated_at")

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		rec := []string{row.RunID, row.EntityType, row.EntityID}
		for _, k := range metricKeys {
			if v, ok := row.Metric(k); ok {
				rec = append(rec, fmt.Sprintf("%.6f", v))
			} else {
				rec = append(rec, "")
			}
		}
		rec = append(rec, strconv.Itoa(row.NActuals), formatTime(row.GeneratedAt))
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func collectMetricKeys(rows []models.MetricRow) []string {
	seen := map[string]struct{}{}
	for _, row := range rows {
		for k := range row.Metrics {
			seen[k] = struct{}{}
		}
	}
	var ordered []string
	for _, k := range PreferredMetricOrder {
		if _, ok := seen[k]; ok {
			ordered = append(ordered, k)
			delete(seen, k)
		}
	}
	var rest []string
	for k := range seen {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// Baseline long header (preferred for pluggable metrics).
var BaselineLongFields = []string{
	"entity_type", "entity_id", "metric", "mean", "std", "window_days",
}

func ReadBaseline(path string) (models.BaselineIndex, error) {
	out := models.BaselineIndex{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := indexMap(header)

	// Long format if "metric" column present.
	if _, ok := idx["metric"]; ok {
		for {
			rec, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			get := func(k string) string {
				i, ok := idx[k]
				if !ok || i >= len(rec) {
					return ""
				}
				return strings.TrimSpace(rec[i])
			}
			mean, _ := strconv.ParseFloat(get("mean"), 64)
			std, _ := strconv.ParseFloat(get("std"), 64)
			window := 28
			if v := get("window_days"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					window = n
				}
			}
			et := get("entity_type")
			if et == "" {
				et = "zone"
			}
			stat := models.BaselineStat{
				EntityType: et,
				EntityID:   get("entity_id"),
				Metric:     get("metric"),
				Mean:       mean,
				Std:        std,
				WindowDays: window,
			}
			if stat.EntityID == "" || stat.Metric == "" {
				continue
			}
			out.Put(stat)
		}
		return out, nil
	}

	// Wide / legacy: mape_mean, mape_std, bias_mean, bias_std, …
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(k string) string {
			i, ok := idx[k]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		et := get("entity_type")
		if et == "" {
			et = "zone"
		}
		entityID := get("entity_id")
		window := 28
		if v := get("window_days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				window = n
			}
		}
		// Discover *_mean columns.
		for col := range idx {
			if !strings.HasSuffix(col, "_mean") {
				continue
			}
			metric := strings.TrimSuffix(col, "_mean")
			meanRaw := get(col)
			stdRaw := get(metric + "_std")
			if meanRaw == "" {
				continue
			}
			mean, err := strconv.ParseFloat(meanRaw, 64)
			if err != nil {
				continue
			}
			std := 0.0
			if stdRaw != "" {
				std, _ = strconv.ParseFloat(stdRaw, 64)
			}
			out.Put(models.BaselineStat{
				EntityType: et,
				EntityID:   entityID,
				Metric:     metric,
				Mean:       mean,
				Std:        std,
				WindowDays: window,
			})
		}
	}
	return out, nil
}

func WriteBaseline(path string, rows []models.BaselineStat) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(BaselineLongFields); err != nil {
		return err
	}
	// Stable order: entity_id, then metric.
	sorted := append([]models.BaselineStat{}, rows...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].EntityID != sorted[j].EntityID {
			return sorted[i].EntityID < sorted[j].EntityID
		}
		return sorted[i].Metric < sorted[j].Metric
	})
	for _, row := range sorted {
		rec := []string{
			row.EntityType,
			row.EntityID,
			row.Metric,
			fmt.Sprintf("%.6f", row.Mean),
			fmt.Sprintf("%.6f", row.Std),
			strconv.Itoa(row.WindowDays),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func IndexFromStats(rows []models.BaselineStat) models.BaselineIndex {
	out := models.BaselineIndex{}
	for _, s := range rows {
		out.Put(s)
	}
	return out
}

func indexMap(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.TrimSpace(h)] = i
	}
	return m
}

func parseTime(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var last error
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
		last = err
	}
	return time.Time{}, last
}

func formatTime(t time.Time) string {
	t = t.UTC()
	return t.Format("2006-01-02T15:04:05") + "+00:00"
}
