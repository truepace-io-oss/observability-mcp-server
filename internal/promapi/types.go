package promapi

import "github.com/prometheus/common/model"

// QueryResult is a decoded /api/v1/query or /query_range result.
type QueryResult struct {
	Type     model.ValueType
	Vector   model.Vector
	Matrix   model.Matrix
	Scalar   *model.Scalar
	String   *model.String
	Warnings []string
}

// Rule mirrors a Prometheus /api/v1/rules rule (alerting or recording).
type RuleGroup struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Rules []Rule `json:"rules"`
}

type Rule struct {
	Name     string            `json:"name"`
	Query    string            `json:"query"`
	Type     string            `json:"type"`             // "alerting" | "recording"
	State    string            `json:"state,omitempty"`  // firing|pending|inactive (alerting)
	Health   string            `json:"health,omitempty"` // ok|err|unknown
	Duration float64           `json:"duration,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Alerts   []Alert           `json:"alerts,omitempty"`
}

// Alert is an active alert from /api/v1/alerts or a rule's alerts list.
type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	ActiveAt    string            `json:"activeAt"`
	Value       string            `json:"value"`
}

// Target is a scrape target from /api/v1/targets.
type Target struct {
	ScrapePool       string            `json:"scrapePool"`
	Labels           map[string]string `json:"labels"`
	DiscoveredLabels map[string]string `json:"discoveredLabels"`
	ScrapeURL        string            `json:"scrapeUrl"`
	Health           string            `json:"health"`
	LastError        string            `json:"lastError"`
	LastScrape       string            `json:"lastScrape"`
}

// MetadataEntry is one metric-metadata record from /api/v1/metadata.
type MetadataEntry struct {
	Type string `json:"type"`
	Help string `json:"help"`
	Unit string `json:"unit"`
}
