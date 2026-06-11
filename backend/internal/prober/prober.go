package prober

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"metriclens/backend/internal/model"
)

const (
	portLabel      = "metriclens.port"
	maxProbeBody   = 1024 * 1024
	defaultTimeout = 2 * time.Second
)

var (
	defaultPaths = []string{"/metrics", "/actuator/prometheus", "/q/metrics"}
	commonPorts  = []int{3000, 5000, 8000, 8080, 9090, 9091}

	sampleLinePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(?:\{[^}\r\n]*\})?\s+[-+]?(?:(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?|Inf|NaN)(?:\s+\d+)?$`)
)

type Prober struct {
	client *http.Client
	now    func() time.Time
}

func NewDefault() *Prober {
	return New(&http.Client{Timeout: defaultTimeout})
}

func New(client *http.Client) *Prober {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Prober{client: client, now: time.Now}
}

func (p *Prober) Probe(ctx context.Context, containers []model.DiscoveredContainer) []model.Target {
	discoveredAt := p.now().UTC().Format(time.RFC3339)
	targets := make([]model.Target, 0, len(containers))

	for _, container := range containers {
		if container.State != model.ContainerStateRunning || container.ComposeService == "" {
			continue
		}
		targets = append(targets, p.probeContainer(ctx, container, discoveredAt))
	}

	return targets
}

func (p *Prober) probeContainer(ctx context.Context, container model.DiscoveredContainer, discoveredAt string) model.Target {
	target := model.Target{
		ID:            container.ID,
		ServiceName:   container.ComposeService,
		ContainerName: container.Name,
		Status:        model.TargetStatusDown,
		DiscoveredAt:  discoveredAt,
	}

	urls, configError := candidateURLs(container)
	if len(urls) == 0 {
		target.LastError = "no probe candidates available"
		return target
	}

	var lastError string
	for _, url := range urls {
		ok, errMessage := p.probeURL(ctx, url)
		if ok {
			target.URL = url
			target.Status = model.TargetStatusUp
			return target
		}
		lastError = errMessage
	}

	target.LastError = downMessage(configError, lastError)
	return target
}

func (p *Prober) probeURL(ctx context.Context, url string) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Sprintf("invalid probe URL %q: %v", url, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false, fmt.Sprintf("GET %s returned HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBody))
	if err != nil {
		return false, fmt.Sprintf("GET %s response read failed: %v", url, err)
	}
	if !LooksPrometheus(body) {
		return false, fmt.Sprintf("GET %s returned non-Prometheus response", url)
	}
	return true, ""
}

func candidateURLs(container model.DiscoveredContainer) ([]string, string) {
	hosts := candidateHosts(container)
	ports, configError := candidatePorts(container)

	urls := make([]string, 0, len(hosts)*len(ports)*len(defaultPaths))
	for _, host := range hosts {
		for _, port := range ports {
			for _, path := range defaultPaths {
				urls = append(urls, fmt.Sprintf("http://%s:%d%s", host, port, path))
			}
		}
	}
	return urls, configError
}

func candidateHosts(container model.DiscoveredContainer) []string {
	seen := map[string]struct{}{}
	hosts := make([]string, 0, 2)

	for _, host := range []string{container.ComposeService, container.Name} {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}

	return hosts
}

func candidatePorts(container model.DiscoveredContainer) ([]int, string) {
	seen := map[int]struct{}{}
	ports := make([]int, 0, 1+len(container.ExposedPorts)+len(commonPorts))
	var configError string

	if rawPort := strings.TrimSpace(container.Labels[portLabel]); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port <= 0 || port > 65535 {
			configError = fmt.Sprintf("invalid %s label %q", portLabel, rawPort)
		} else {
			ports = appendPort(ports, seen, port)
		}
	}

	for _, port := range container.ExposedPorts {
		ports = appendPort(ports, seen, port)
	}
	for _, port := range commonPorts {
		ports = appendPort(ports, seen, port)
	}

	return ports, configError
}

func appendPort(ports []int, seen map[int]struct{}, port int) []int {
	if port <= 0 || port > 65535 {
		return ports
	}
	if _, ok := seen[port]; ok {
		return ports
	}
	seen[port] = struct{}{}
	return append(ports, port)
}

func downMessage(configError, lastError string) string {
	parts := []string{"no Prometheus endpoint found"}
	if configError != "" {
		parts = append(parts, configError)
	}
	if lastError != "" {
		parts = append(parts, "last probe: "+lastError)
	}
	return strings.Join(parts, "; ")
}

func LooksPrometheus(body []byte) bool {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return false
	}

	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.HasPrefix(lower, "{") ||
		strings.HasPrefix(lower, "[") {
		return false
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# HELP ") || strings.HasPrefix(line, "# TYPE ") {
			return true
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if sampleLinePattern.MatchString(line) {
			return true
		}
	}

	return false
}
