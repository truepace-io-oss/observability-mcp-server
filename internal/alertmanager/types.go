package alertmanager

// Matcher is an Alertmanager v2 label matcher.
type Matcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual *bool  `json:"isEqual,omitempty"`
}

// GettableAlert is an alert as returned by /api/v2/alerts.
type GettableAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
	EndsAt      string            `json:"endsAt"`
	UpdatedAt   string            `json:"updatedAt"`
	Fingerprint string            `json:"fingerprint"`
	Status      struct {
		State       string   `json:"state"`
		SilencedBy  []string `json:"silencedBy"`
		InhibitedBy []string `json:"inhibitedBy"`
	} `json:"status"`
	Receivers []Receiver `json:"receivers"`
}

// AlertGroup groups alerts by labels.
type AlertGroup struct {
	Labels   map[string]string `json:"labels"`
	Receiver Receiver          `json:"receiver"`
	Alerts   []GettableAlert   `json:"alerts"`
}

// Receiver is a notification receiver.
type Receiver struct {
	Name string `json:"name"`
}

// GettableSilence is a silence as returned by /api/v2/silences.
type GettableSilence struct {
	ID        string                 `json:"id"`
	Status    struct{ State string } `json:"status"`
	Matchers  []Matcher              `json:"matchers"`
	StartsAt  string                 `json:"startsAt"`
	EndsAt    string                 `json:"endsAt"`
	CreatedBy string                 `json:"createdBy"`
	Comment   string                 `json:"comment"`
}

// PostableSilence is the body for creating a silence via /api/v2/silences.
type PostableSilence struct {
	ID        string    `json:"id,omitempty"`
	Matchers  []Matcher `json:"matchers"`
	StartsAt  string    `json:"startsAt"`
	EndsAt    string    `json:"endsAt"`
	CreatedBy string    `json:"createdBy"`
	Comment   string    `json:"comment"`
}
