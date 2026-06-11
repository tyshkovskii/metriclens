package discovery

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

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
	ContainerList(context.Context, container.ListOptions) ([]container.Summary, error)
}

func NewDockerDiscovery() (*DockerDiscovery, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	return &DockerDiscovery{client: cli, selfID: hostname}, nil
}

func (d *DockerDiscovery) ListContainers(ctx context.Context) ([]model.DiscoveredContainer, error) {
	containers, err := d.client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}

	discovered := make([]model.DiscoveredContainer, 0, len(containers))
	for _, c := range containers {
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

func exposedPorts(ports []container.Port) []int {
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
