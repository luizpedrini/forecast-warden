package csvio

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/models"
)

var MetricsFields = []string{
	"run_id", "entity_type", "entity_id", "mape", "bias", "coverage_80", "n_actuals", "generated_at",
}

var BaselineFields = []string{
	"entity_type", "entity_id", "mape_mean", "mape_std", "bias_mean", "bias_std", "window_days",
}

func ReadMetrics(path string) ([]models.MetricRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := indexMap(header)
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
			return rec[i]
		}
		mape, err := strconv.ParseFloat(get("mape"), 64)
		if err != nil {
			return nil, fmt.Errorf("mape: %w", err)
		}
		bias, err := strconv.ParseFloat(get("bias"), 64)
		if err != nil {
			return nil, fmt.Errorf("bias: %w", err)
		}
		cov, err := strconv.ParseFloat(get("coverage_80"), 64)
		if err != nil {
			return nil, fmt.Errorf("coverage_80: %w", err)
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
		rows = append(rows, models.MetricRow{
			RunID:       get("run_id"),
			EntityType:  et,
			EntityID:    get("entity_id"),
			MAPE:        mape,
			Bias:        bias,
			Coverage80:  cov,
			NActuals:    n,
			GeneratedAt: ts,
		})
	}
	return rows, nil
}

func WriteMetrics(path string, rows []models.MetricRow) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(MetricsFields); err != nil {
		return err
	}
	for _, row := range rows {
		rec := []string{
			row.RunID,
			row.EntityType,
			row.EntityID,
			fmt.Sprintf("%.6f", row.MAPE),
			fmt.Sprintf("%.6f", row.Bias),
			fmt.Sprintf("%.6f", row.Coverage80),
			strconv.Itoa(row.NActuals),
			formatTime(row.GeneratedAt),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func ReadBaseline(path string) (map[string]models.BaselineStats, error) {
	out := map[string]models.BaselineStats{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := indexMap(header)
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
			return rec[i]
		}
		mapeMean, _ := strconv.ParseFloat(get("mape_mean"), 64)
		mapeStd, _ := strconv.ParseFloat(get("mape_std"), 64)
		biasMean, _ := strconv.ParseFloat(get("bias_mean"), 64)
		biasStd, _ := strconv.ParseFloat(get("bias_std"), 64)
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
		stats := models.BaselineStats{
			EntityType: et,
			EntityID:   get("entity_id"),
			MAPEMean:   mapeMean,
			MAPEStd:    mapeStd,
			BiasMean:   biasMean,
			BiasStd:    biasStd,
			WindowDays: window,
		}
		out[stats.EntityID] = stats
	}
	return out, nil
}

func WriteBaseline(path string, rows []models.BaselineStats) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(BaselineFields); err != nil {
		return err
	}
	for _, row := range rows {
		rec := []string{
			row.EntityType,
			row.EntityID,
			fmt.Sprintf("%.6f", row.MAPEMean),
			fmt.Sprintf("%.6f", row.MAPEStd),
			fmt.Sprintf("%.6f", row.BiasMean),
			fmt.Sprintf("%.6f", row.BiasStd),
			strconv.Itoa(row.WindowDays),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func indexMap(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[h] = i
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
	// Match Python datetime.isoformat() for aware UTC: +00:00
	t = t.UTC()
	return t.Format("2006-01-02T15:04:05") + "+00:00"
}
