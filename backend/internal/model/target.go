package model

type TargetStatus string

const (
	TargetStatusUp   TargetStatus = "up"
	TargetStatusDown TargetStatus = "down"
)

type Target struct {
	ID                 string       `json:"id"`
	ServiceName        string       `json:"serviceName"`
	ContainerName      string       `json:"containerName"`
	URL                string       `json:"url,omitempty"`
	Status             TargetStatus `json:"status"`
	LastError          string       `json:"lastError,omitempty"`
	LastScrapeAt       string       `json:"lastScrapeAt,omitempty"`
	LastScrapeDuration string       `json:"lastScrapeDuration,omitempty"`
	DiscoveredAt       string       `json:"discoveredAt"`
}

type TargetMetricsResponse struct {
	Target   Target         `json:"target"`
	Families []MetricFamily `json:"families"`
}
