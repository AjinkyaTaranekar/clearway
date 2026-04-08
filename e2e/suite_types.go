package e2e

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var defaultPublicBaseURLs = []string{
	"http://35.187.121.12", // EU (eu-west1) from README
}

// Config controls how the end-to-end suite runs.
type Config struct {
	BaseURLs       []string
	RequestTimeout time.Duration
	RunTag         string
	Password       string
	EmailDomain    string
	ReportDir      string
	SkipIfUnhealthy bool
}

func loadConfig() Config {
	baseURLs := splitAndClean(os.Getenv("E2E_BASE_URLS"))
	if len(baseURLs) == 0 {
		baseURLs = append([]string{}, defaultPublicBaseURLs...)
	}

	timeoutSeconds := parseIntWithDefault(os.Getenv("E2E_REQUEST_TIMEOUT_SECONDS"), 20)
	if timeoutSeconds < 5 {
		timeoutSeconds = 5
	}

	runTag := strings.TrimSpace(os.Getenv("E2E_RUN_TAG"))
	if runTag == "" {
		runTag = time.Now().UTC().Format("20060102")
	}

	password := strings.TrimSpace(os.Getenv("E2E_PASSWORD"))
	if password == "" {
		password = "E2Epass123"
	}

	emailDomain := strings.TrimSpace(os.Getenv("E2E_EMAIL_DOMAIN"))
	if emailDomain == "" {
		emailDomain = "example.com"
	}

	reportDir := strings.TrimSpace(os.Getenv("E2E_REPORT_DIR"))
	if reportDir == "" {
		reportDir = "reports"
	}

	skipIfUnhealthy := true
	if raw := strings.TrimSpace(os.Getenv("E2E_SKIP_IF_UNHEALTHY")); raw != "" {
		skipIfUnhealthy = strings.EqualFold(raw, "true") || raw == "1" || strings.EqualFold(raw, "yes")
	}

	return Config{
		BaseURLs:        baseURLs,
		RequestTimeout:  time.Duration(timeoutSeconds) * time.Second,
		RunTag:          runTag,
		Password:        password,
		EmailDomain:     emailDomain,
		ReportDir:       reportDir,
		SkipIfUnhealthy: skipIfUnhealthy,
	}
}

func (c Config) emailFor(alias string) string {
	cleanAlias := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(alias), " ", "-"))
	cleanAlias = strings.ReplaceAll(cleanAlias, "_", "-")
	cleanAlias = strings.ReplaceAll(cleanAlias, ".", "-")
	return fmt.Sprintf("e2e-%s-%s@%s", cleanAlias, c.RunTag, c.EmailDomain)
}

func splitAndClean(raw string) []string {
	parts := strings.Split(raw, ",")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			cleaned = append(cleaned, strings.TrimRight(trimmed, "/"))
		}
	}
	return cleaned
}

func parseIntWithDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func newLogger() *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h)
}

// HTTPTrace records one outbound API call.
type HTTPTrace struct {
	RequestName   string `json:"request_name"`
	Method        string `json:"method"`
	URL           string `json:"url"`
	StatusCode    int    `json:"status_code"`
	DurationMS    int64  `json:"duration_ms"`
	TraceID       string `json:"trace_id,omitempty"`
	UsedAuthToken bool   `json:"used_auth_token"`
	RequestBody   string `json:"request_body,omitempty"`
	ResponseBody  string `json:"response_body,omitempty"`
}

// StepRecord is one assertion/action inside a scenario.
type StepRecord struct {
	Name        string     `json:"name"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt time.Time  `json:"completed_at"`
	DurationMS  int64      `json:"duration_ms"`
	Passed      bool       `json:"passed"`
	Error       string     `json:"error,omitempty"`
	Details     string     `json:"details,omitempty"`
	HTTP        *HTTPTrace `json:"http,omitempty"`
}

// ScenarioRecord captures the full result for a scenario.
type ScenarioRecord struct {
	Name        string                 `json:"name"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt time.Time              `json:"completed_at"`
	DurationMS  int64                  `json:"duration_ms"`
	Passed      bool                   `json:"passed"`
	Steps       []StepRecord           `json:"steps"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// RunReport is the centralized report for the entire end-to-end flow.
type RunReport struct {
	RunID          string           `json:"run_id"`
	StartedAt      time.Time        `json:"started_at"`
	CompletedAt    time.Time        `json:"completed_at"`
	DurationMS     int64            `json:"duration_ms"`
	BaseURLs       []string         `json:"base_urls"`
	RegionsByURL   map[string]string `json:"regions_by_url,omitempty"`
	Scenarios      []ScenarioRecord `json:"scenarios"`
	TotalScenarios int              `json:"total_scenarios"`
	PassedScenarios int             `json:"passed_scenarios"`
	FailedScenarios int             `json:"failed_scenarios"`
}

// Reporter coordinates scenario-level reporting.
type Reporter struct {
	mu      sync.Mutex
	logger  *slog.Logger
	report  RunReport
	reportDir string
}

func newReporter(cfg Config, logger *slog.Logger) *Reporter {
	now := time.Now().UTC()
	return &Reporter{
		logger: logger,
		report: RunReport{
			RunID:       fmt.Sprintf("e2e-%s-%d", cfg.RunTag, now.Unix()),
			StartedAt:   now,
			BaseURLs:    append([]string{}, cfg.BaseURLs...),
			RegionsByURL: map[string]string{},
			Scenarios:   make([]ScenarioRecord, 0, 8),
		},
		reportDir: cfg.ReportDir,
	}
}

func (r *Reporter) setRegion(baseURL, region string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.RegionsByURL[baseURL] = region
}

func (r *Reporter) beginScenario(name string) *ScenarioRecorder {
	return &ScenarioRecorder{
		reporter: r,
		record: ScenarioRecord{
			Name:      name,
			StartedAt: time.Now().UTC(),
			Passed:    true,
			Steps:     make([]StepRecord, 0, 32),
			Metadata:  map[string]interface{}{},
		},
	}
}

func (r *Reporter) appendScenario(record ScenarioRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Scenarios = append(r.report.Scenarios, record)
}

func (r *Reporter) writeJSON() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.report.CompletedAt = time.Now().UTC()
	r.report.DurationMS = r.report.CompletedAt.Sub(r.report.StartedAt).Milliseconds()
	r.report.TotalScenarios = len(r.report.Scenarios)
	r.report.PassedScenarios = 0
	r.report.FailedScenarios = 0
	for _, scenario := range r.report.Scenarios {
		if scenario.Passed {
			r.report.PassedScenarios++
		} else {
			r.report.FailedScenarios++
		}
	}

	if err := os.MkdirAll(r.reportDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create report directory: %w", err)
	}

	fileName := fmt.Sprintf("%s.json", r.report.RunID)
	filePath := filepath.Join(r.reportDir, fileName)

	payload, err := json.MarshalIndent(r.report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(filePath, payload, 0o644); err != nil {
		return "", fmt.Errorf("failed to write report: %w", err)
	}

	return filePath, nil
}

// ScenarioRecorder tracks one scenario and its detailed steps.
type ScenarioRecorder struct {
	reporter *Reporter
	record   ScenarioRecord
}

// StepContext allows a step to attach HTTP details and human-readable notes.
type StepContext struct {
	httpTrace *HTTPTrace
	details   string
}

func (s *StepContext) AttachHTTP(result *HTTPResult) {
	if result == nil {
		return
	}
	s.httpTrace = result.ToTrace()
}

func (s *StepContext) SetDetails(details string) {
	s.details = strings.TrimSpace(details)
}

func (sr *ScenarioRecorder) setMetadata(key string, value interface{}) {
	sr.record.Metadata[key] = value
}

func (sr *ScenarioRecorder) runStep(name string, fn func(*StepContext) error) {
	ctx := &StepContext{}
	started := time.Now().UTC()
	err := fn(ctx)
	completed := time.Now().UTC()

	record := StepRecord{
		Name:        name,
		StartedAt:   started,
		CompletedAt: completed,
		DurationMS:  completed.Sub(started).Milliseconds(),
		Passed:      err == nil,
		Details:     ctx.details,
		HTTP:        ctx.httpTrace,
	}
	if err != nil {
		record.Error = err.Error()
		sr.record.Passed = false
	}

	sr.record.Steps = append(sr.record.Steps, record)
}

func (sr *ScenarioRecorder) complete() {
	sr.record.CompletedAt = time.Now().UTC()
	sr.record.DurationMS = sr.record.CompletedAt.Sub(sr.record.StartedAt).Milliseconds()
	sr.reporter.appendScenario(sr.record)
}
