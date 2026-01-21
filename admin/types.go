package admin

import "time"

// Ptr returns a pointer to the given value.
// Use this for setting optional fields in update inputs.
func Ptr[T any](v T) *T {
	return &v
}

// Page represents a paginated response.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ListParams contains common pagination parameters.
type ListParams struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Project represents a Tripswitch project.
type Project struct {
	ID                  string `json:"project_id"`
	Name                string `json:"name"`
	SlackWebhookURL     string `json:"slack_webhook_url,omitempty"`
	TraceIDURLTemplate  string `json:"trace_id_url_template,omitempty"`
	EnableSignedIngest  bool   `json:"enable_signed_ingest"`
}

// UpdateProjectInput contains fields for updating a project.
// Use Ptr() to set optional fields.
type UpdateProjectInput struct {
	Name               *string `json:"name,omitempty"`
	SlackWebhookURL    *string `json:"slack_webhook_url,omitempty"`
	TraceIDURLTemplate *string `json:"trace_id_url_template,omitempty"`
	EnableSignedIngest *bool   `json:"enable_signed_ingest,omitempty"`
}

// IngestSecretRotation represents the result of rotating an ingest secret.
type IngestSecretRotation struct {
	IngestSecret string `json:"ingest_secret"`
}

// BreakerKind represents the aggregation type for a breaker.
type BreakerKind string

const (
	BreakerKindErrorRate           BreakerKind = "error_rate"
	BreakerKindAvg                 BreakerKind = "avg"
	BreakerKindP95                 BreakerKind = "p95"
	BreakerKindMax                 BreakerKind = "max"
	BreakerKindMin                 BreakerKind = "min"
	BreakerKindSum                 BreakerKind = "sum"
	BreakerKindStddev              BreakerKind = "stddev"
	BreakerKindCount               BreakerKind = "count"
	BreakerKindPercentile          BreakerKind = "percentile"
	BreakerKindConsecutiveFailures BreakerKind = "consecutive_failures"
	BreakerKindDelta               BreakerKind = "delta"
)

// BreakerOp represents a comparison operator.
type BreakerOp string

const (
	BreakerOpGt  BreakerOp = "gt"
	BreakerOpLt  BreakerOp = "lt"
	BreakerOpGte BreakerOp = "gte"
	BreakerOpLte BreakerOp = "lte"
)

// HalfOpenPolicy represents the policy for half-open state with insufficient data.
type HalfOpenPolicy string

const (
	HalfOpenPolicyOptimistic   HalfOpenPolicy = "optimistic"
	HalfOpenPolicyConservative HalfOpenPolicy = "conservative"
	HalfOpenPolicyPessimistic  HalfOpenPolicy = "pessimistic"
)

// Breaker represents a circuit breaker configuration.
type Breaker struct {
	ID                 string            `json:"id"`
	RouterID           string            `json:"router_id,omitempty"`
	Name               string            `json:"name"`
	Metric             string            `json:"metric"`
	Kind               BreakerKind       `json:"kind"`
	KindParams         map[string]any    `json:"kind_params,omitempty"`
	Op                 BreakerOp         `json:"op"`
	Threshold          float64           `json:"threshold"`
	WindowMs           int               `json:"window_ms,omitempty"`
	MinCount           int               `json:"min_count,omitempty"`
	MinStateDurationMs int               `json:"min_state_duration_ms,omitempty"`
	CooldownMs         int               `json:"cooldown_ms,omitempty"`
	Actions            map[string]any    `json:"actions,omitempty"`
}

// CreateBreakerInput contains fields for creating a breaker.
type CreateBreakerInput struct {
	Name               string         `json:"name"`
	Metric             string         `json:"metric"`
	Kind               BreakerKind    `json:"kind"`
	KindParams         map[string]any `json:"kind_params,omitempty"`
	Op                 BreakerOp      `json:"op"`
	Threshold          float64        `json:"threshold"`
	WindowMs           int            `json:"window_ms,omitempty"`
	MinCount           int            `json:"min_count,omitempty"`
	MinStateDurationMs int            `json:"min_state_duration_ms,omitempty"`
	CooldownMs         int            `json:"cooldown_ms,omitempty"`
	Actions            map[string]any `json:"actions,omitempty"`
}

// UpdateBreakerInput contains fields for updating a breaker.
// Use Ptr() to set optional fields.
type UpdateBreakerInput struct {
	Name               *string         `json:"name,omitempty"`
	Metric             *string         `json:"metric,omitempty"`
	Kind               *BreakerKind    `json:"kind,omitempty"`
	KindParams         map[string]any  `json:"kind_params,omitempty"`
	Op                 *BreakerOp      `json:"op,omitempty"`
	Threshold          *float64        `json:"threshold,omitempty"`
	WindowMs           *int            `json:"window_ms,omitempty"`
	MinCount           *int            `json:"min_count,omitempty"`
	MinStateDurationMs *int            `json:"min_state_duration_ms,omitempty"`
	CooldownMs         *int            `json:"cooldown_ms,omitempty"`
	Actions            map[string]any  `json:"actions,omitempty"`
}

// SyncBreakersInput contains a list of breakers for bulk sync.
type SyncBreakersInput struct {
	Breakers []CreateBreakerInput `json:"breakers"`
}

// BreakerState represents the current state of a circuit breaker.
type BreakerState struct {
	BreakerID string  `json:"breaker_id"`
	State     string  `json:"state"` // "open", "closed", "half_open"
	AllowRate float64 `json:"allow_rate"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BatchGetBreakerStatesInput contains parameters for batch state retrieval.
type BatchGetBreakerStatesInput struct {
	BreakerIDs []string `json:"breaker_ids,omitempty"`
	RouterID   string   `json:"router_id,omitempty"`
}

// ListBreakersResponse contains the response from listing breakers.
type ListBreakersResponse struct {
	Breakers  []Breaker `json:"breakers"`
	Count     int       `json:"count"`
	Hash      string    `json:"hash,omitempty"`
	UpdatedAt string    `json:"updated_at,omitempty"` // Non-RFC3339 format from API
}

// RouterMode represents the routing mode.
type RouterMode string

const (
	RouterModeStatic   RouterMode = "static"
	RouterModeCanary   RouterMode = "canary"
	RouterModeWeighted RouterMode = "weighted"
)

// Router represents a router configuration.
type Router struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Mode         RouterMode `json:"mode"`
	Enabled      bool       `json:"enabled"`
	BreakerCount int        `json:"breaker_count,omitempty"`
	Breakers     []Breaker  `json:"breakers,omitempty"`
	InsertedAt   time.Time  `json:"inserted_at,omitempty"`
	CreatedBy    string     `json:"created_by,omitempty"`
}

// ListRoutersResponse contains the response from listing routers.
type ListRoutersResponse struct {
	Routers []Router `json:"routers"`
}

// CreateRouterInput contains fields for creating a router.
type CreateRouterInput struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Mode        RouterMode `json:"mode"`
	Enabled     bool       `json:"enabled,omitempty"`
}

// UpdateRouterInput contains fields for updating a router.
// Use Ptr() to set optional fields.
type UpdateRouterInput struct {
	Name        *string     `json:"name,omitempty"`
	Description *string     `json:"description,omitempty"`
	Mode        *RouterMode `json:"mode,omitempty"`
	Enabled     *bool       `json:"enabled,omitempty"`
}

// LinkBreakerInput contains parameters for linking a breaker to a router.
type LinkBreakerInput struct {
	BreakerID string `json:"breaker_id"`
}

// NotificationChannelType represents the type of notification channel.
type NotificationChannelType string

const (
	NotificationChannelSlack     NotificationChannelType = "slack"
	NotificationChannelPagerDuty NotificationChannelType = "pagerduty"
	NotificationChannelEmail     NotificationChannelType = "email"
	NotificationChannelWebhook   NotificationChannelType = "webhook"
)

// NotificationEventType represents the event types that trigger notifications.
type NotificationEventType string

const (
	NotificationEventTrip    NotificationEventType = "trip"
	NotificationEventRecover NotificationEventType = "recover"
)

// NotificationChannel represents a notification channel configuration.
type NotificationChannel struct {
	ID        string                  `json:"id"`
	ProjectID string                  `json:"project_id"`
	Name      string                  `json:"name"`
	Channel   NotificationChannelType `json:"channel"`
	Config    map[string]any          `json:"config"`
	Events    []NotificationEventType `json:"events"`
	Enabled   bool                    `json:"enabled"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// CreateNotificationChannelInput contains fields for creating a notification channel.
type CreateNotificationChannelInput struct {
	Name    string                  `json:"name"`
	Channel NotificationChannelType `json:"channel"`
	Config  map[string]any          `json:"config"`
	Events  []NotificationEventType `json:"events"`
	Enabled bool                    `json:"enabled,omitempty"`
}

// UpdateNotificationChannelInput contains fields for updating a notification channel.
// Use Ptr() to set optional fields.
type UpdateNotificationChannelInput struct {
	Name    *string                  `json:"name,omitempty"`
	Config  map[string]any           `json:"config,omitempty"`
	Events  []NotificationEventType  `json:"events,omitempty"`
	Enabled *bool                    `json:"enabled,omitempty"`
}

// Event represents a breaker state transition event.
type Event struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	BreakerID string    `json:"breaker_id"`
	FromState string    `json:"from_state"`
	ToState   string    `json:"to_state"`
	Reason    string    `json:"reason,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ListEventsParams contains parameters for listing events.
type ListEventsParams struct {
	BreakerID string    `json:"breaker_id,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Cursor    string    `json:"cursor,omitempty"`
	Limit     int       `json:"limit,omitempty"`
}

// ListEventsResponse contains the response from listing events.
type ListEventsResponse struct {
	Events     []Event `json:"events"`
	Returned   int     `json:"returned"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

// Status represents the project status summary.
type Status struct {
	OpenCount   int   `json:"open_count"`
	ClosedCount int   `json:"closed_count"`
	LastEvalMs  int64 `json:"last_eval_ms,omitempty"`
}
