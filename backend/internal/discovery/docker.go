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

	discovered := make([]model.DiscoveredContainer, 0, len(result.Items))
	for _, c := range result.Items {
		if d.excluded(c) {
			continue
		}
		discovered = append(discovered, FromDockerContainer(c))
	}
	return discovered, nil
}

func (d *DockerDiscovery) excluded(c container.Summary) bool {
	if c.Labels[excludeLabel] == "true" {
		return true
	}
	// Inside a container the hostname defaults to the short container ID,
	// so a prefix match identifies the container metriclens itself runs in.
	return d.selfID != "" && strings.HasPrefix(c.ID, d.selfID)
}

func FromDockerContainer(c container.Summary) model.DiscoveredContainer {
	labels := c.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	return model.DiscoveredContainer{
		ID:             c.ID,
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
