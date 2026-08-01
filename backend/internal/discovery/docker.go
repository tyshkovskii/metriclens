package discovery

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"metriclens/backend/internal/model"
)

const (
	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
	composeNumberLabel  = "com.docker.compose.container-number"
	excludeLabel        = "metriclens.exclude"
)

type DockerDiscovery struct {
	client dockerClient
	selfID string
}

type dockerClient interface {
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
}

func NewDockerDiscovery() (*DockerDiscovery, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("read hostname: %w", err)
	}
	return &DockerDiscovery{client: cli, selfID: hostname}, nil
}

func (d *DockerDiscovery) ListContainers(ctx context.Context) ([]model.DiscoveredContainer, error) {
	result, err := d.client.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return nil, err
	}

	composeProject := d.selfComposeProject(result.Items)
	discovered := make([]model.DiscoveredContainer, 0, len(result.Items))
	for _, c := range result.Items {
		if d.excluded(c) || (composeProject != "" && c.Labels[composeProjectLabel] != composeProject) {
			continue
		}
		discovered = append(discovered, FromDockerContainer(c))
	}
	return discovered, nil
}

// selfComposeProject identifies the Compose project that owns MetricLens when
// the process itself is running in a Compose container. If the self container
// cannot be matched (for example when MetricLens runs on the host), discovery
// remains unscoped.
func (d *DockerDiscovery) selfComposeProject(containers []container.Summary) string {
	for _, c := range containers {
		if !d.isSelf(c) {
			continue
		}
		return strings.TrimSpace(c.Labels[composeProjectLabel])
	}
	return ""
}

func (d *DockerDiscovery) isSelf(c container.Summary) bool {
	selfID := strings.TrimPrefix(strings.TrimSpace(d.selfID), "/")
	containerID := strings.TrimPrefix(strings.TrimSpace(c.ID), "/")
	if selfID == "" || containerID == "" {
		return false
	}
	return strings.HasPrefix(containerID, selfID) || strings.HasPrefix(selfID, containerID)
}

func (d *DockerDiscovery) excluded(c container.Summary) bool {
	if c.Labels[excludeLabel] == "true" {
		return true
	}
	// Inside a container the hostname defaults to the short container ID,
	// so a prefix match identifies the container metriclens itself runs in.
	return d.isSelf(c)
}

func FromDockerContainer(c container.Summary) model.DiscoveredContainer {
	labels := c.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	return model.DiscoveredContainer{
		ID:             c.ID,
		HistoryID:      historyIdentity(c),
		Name:           cleanContainerName(c.Names),
		Image:          c.Image,
		State:          normalizeState(string(c.State)),
		ComposeProject: labels[composeProjectLabel],
		ComposeService: labels[composeServiceLabel],
		Networks:       networkNames(c),
		ExposedPorts:   exposedPorts(c.Ports),
		Labels:         labels,
	}
}

// historyIdentity prefers Compose's project/service/replica identity so a
// recreated container continues the same retained series. Container ID is a
// safe fallback whenever the labels needed to establish that identity are not
// available.
func historyIdentity(c container.Summary) string {
	project := strings.TrimSpace(c.Labels[composeProjectLabel])
	service := strings.TrimSpace(c.Labels[composeServiceLabel])
	number := strings.TrimSpace(c.Labels[composeNumberLabel])
	if project == "" || service == "" || number == "" {
		return c.ID
	}
	return "compose:" + project + "/" + service + "/" + number
}

// HistoryIdentity exposes the stable identity calculation for packages that
// receive an already-normalized container.
func HistoryIdentity(c model.DiscoveredContainer) string {
	if c.HistoryID != "" {
		return c.HistoryID
	}
	project := strings.TrimSpace(c.ComposeProject)
	service := strings.TrimSpace(c.ComposeService)
	number := strings.TrimSpace(c.Labels[composeNumberLabel])
	if project == "" || service == "" || number == "" {
		return c.ID
	}
	return "compose:" + project + "/" + service + "/" + number
}

func cleanContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func normalizeState(state string) model.ContainerState {
	switch state {
	case string(model.ContainerStateRunning):
		return model.ContainerStateRunning
	case string(model.ContainerStateExited):
		return model.ContainerStateExited
	default:
		return model.ContainerStateUnknown
	}
}

func networkNames(c container.Summary) []string {
	if c.NetworkSettings == nil {
		return []string{}
	}

	names := make([]string, 0, len(c.NetworkSettings.Networks))
	for name := range c.NetworkSettings.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func exposedPorts(ports []container.PortSummary) []int {
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if port.PrivatePort == 0 {
			continue
		}
		seen[int(port.PrivatePort)] = struct{}{}
	}

	result := make([]int, 0, len(seen))
	for port := range seen {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}
