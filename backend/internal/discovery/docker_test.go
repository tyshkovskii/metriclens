package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type fakeDockerClient struct {
	containers []container.Summary
	states     map[string]*container.State
}

func (f fakeDockerClient) ContainerList(_ context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	items := make([]container.Summary, 0, len(f.containers))
	for _, c := range f.containers {
		if options.All || c.State == "" || c.State == container.StateRunning {
			items = append(items, c)
		}
	}
	return client.ContainerListResult{Items: items}, nil
}

func (f fakeDockerClient) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	state, ok := f.states[id]
	if !ok {
		return client.ContainerInspectResult{}, errors.New("no such container")
	}
	return client.ContainerInspectResult{Container: container.InspectResponse{State: state}}, nil
}

func TestContainerExitAndStoppedServicesExplainStoppedContainers(t *testing.T) {
	compose := func(id, service string, state container.ContainerState) container.Summary {
		return container.Summary{ID: id, State: state, Labels: map[string]string{composeProjectLabel: "shop", composeServiceLabel: service}}
	}
	oneOff := compose("api-run", "api", container.StateExited)
	oneOff.Labels[composeOneOffLabel] = "True"
	d := &DockerDiscovery{client: fakeDockerClient{
		containers: []container.Summary{
			compose("api", "api", container.StateRunning),
			oneOff,
			compose("worker", "worker", container.StateExited),
			compose("cron", "cron", container.StateRestarting),
			compose("db", "db", container.StateExited),
		},
		states: map[string]*container.State{
			"api":     {Status: container.StateRunning, Running: true},
			"api-run": {Status: container.StateExited, ExitCode: 2},
			"worker":  {Status: container.StateExited, ExitCode: 137, OOMKilled: true},
			"cron":    {Status: container.StateRestarting, Running: true, Restarting: true, ExitCode: 1},
			"db":      {Status: container.StateExited},
		},
	}}

	if reason, ok := d.ContainerExit(context.Background(), "api"); ok {
		t.Fatalf("running container reason = %q, want none", reason)
	}
	if _, ok := d.ContainerExit(context.Background(), "removed"); ok {
		t.Fatal("a removed container should have no exit reason")
	}
	stopped, err := d.StoppedServices(context.Background(), []string{"api", "worker", "cron"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"worker": "container exited with code 137, OOM killed",
		"cron":   "container is restarting after exiting with code 1",
	}
	if !reflect.DeepEqual(stopped, want) {
		t.Fatalf("stopped = %#v, want %#v", stopped, want)
	}
}

func TestListContainersExcludesSelf(t *testing.T) {
	d := &DockerDiscovery{
		client: fakeDockerClient{containers: []container.Summary{
			{ID: "abcdef123456beefbeefbeef", Names: []string{"/metriclens-1"}},
			{ID: "111111111111aaaaaaaaaaaa", Names: []string{"/example-api-1"}},
		}},
		selfID: "abcdef123456",
	}

	got, err := d.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "example-api-1" {
		t.Fatalf("containers = %#v, want only example-api-1", got)
	}
}

func TestListContainersScopesToSelfComposeProject(t *testing.T) {
	d := &DockerDiscovery{
		client: fakeDockerClient{containers: []container.Summary{
			{ID: "abcdef123456beefbeefbeef", Labels: map[string]string{
				composeProjectLabel: "app",
			}},
			{ID: "111111111111", Names: []string{"/same-1"}, Labels: map[string]string{
				composeProjectLabel: "app",
			}},
			{ID: "222222222222", Names: []string{"/other-1"}, Labels: map[string]string{
				composeProjectLabel: "other",
			}},
			{ID: "333333333333", Names: []string{"/unscoped"}},
		}},
		selfID: "abcdef123456",
	}

	got, err := d.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "same-1" {
		t.Fatalf("containers = %#v, want only same-1", got)
	}
}

func TestListContainersFallsBackToUnscopedWhenSelfProjectUnavailable(t *testing.T) {
	d := &DockerDiscovery{
		client: fakeDockerClient{containers: []container.Summary{
			{ID: "abcdef123456beefbeefbeef"},
			{ID: "111111111111", Names: []string{"/other-1"}, Labels: map[string]string{
				composeProjectLabel: "other",
			}},
		}},
		selfID: "abcdef123456",
	}

	got, err := d.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "other-1" {
		t.Fatalf("containers = %#v, want other-1", got)
	}
}

func TestListContainersExcludesLabeled(t *testing.T) {
	d := &DockerDiscovery{
		client: fakeDockerClient{containers: []container.Summary{
			{ID: "111111111111", Names: []string{"/hidden-1"}, Labels: map[string]string{"metriclens.exclude": "true"}},
			{ID: "222222222222", Names: []string{"/visible-1"}},
		}},
		selfID: "not-a-container",
	}

	got, err := d.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "visible-1" {
		t.Fatalf("containers = %#v, want only visible-1", got)
	}
}

func TestFromDockerContainerExtractsComposeMetadata(t *testing.T) {
	got := FromDockerContainer(container.Summary{
		ID:    "abc123",
		Names: []string{"/example-api-1"},
		Image: "example/api:latest",
		State: "running",
		Labels: map[string]string{
			"com.docker.compose.project":          "example",
			"com.docker.compose.service":          "api",
			"com.docker.compose.container-number": "1",
		},
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"z_net": nil,
				"a_net": nil,
			},
		},
		Ports: []container.PortSummary{
			{PrivatePort: 8080},
			{PrivatePort: 8080},
			{PrivatePort: 9090},
		},
	})

	if got.ID != "abc123" {
		t.Fatalf("id = %q, want abc123", got.ID)
	}
	if got.Name != "example-api-1" {
		t.Fatalf("name = %q, want example-api-1", got.Name)
	}
	if got.ComposeProject != "example" {
		t.Fatalf("composeProject = %q, want example", got.ComposeProject)
	}
	if got.ComposeService != "api" {
		t.Fatalf("composeService = %q, want api", got.ComposeService)
	}
	if got.HistoryID != "compose:example/api/1" {
		t.Fatalf("history id = %q, want compose:example/api/1", got.HistoryID)
	}
	if got.State != "running" {
		t.Fatalf("state = %q, want running", got.State)
	}
	if !reflect.DeepEqual(got.Networks, []string{"a_net", "z_net"}) {
		t.Fatalf("networks = %#v, want sorted names", got.Networks)
	}
	if !reflect.DeepEqual(got.ExposedPorts, []int{8080, 9090}) {
		t.Fatalf("exposedPorts = %#v, want [8080 9090]", got.ExposedPorts)
	}
}

func TestFromDockerContainerHistoryIdentityFallsBackToContainerID(t *testing.T) {
	got := FromDockerContainer(container.Summary{
		ID: "abc123",
		Labels: map[string]string{
			composeProjectLabel: "example",
			composeServiceLabel: "api",
		},
	})
	if got.HistoryID != got.ID {
		t.Fatalf("history id = %q, want container id %q", got.HistoryID, got.ID)
	}
}

func TestFromDockerContainerUnknownState(t *testing.T) {
	got := FromDockerContainer(container.Summary{State: "paused"})

	if got.State != "unknown" {
		t.Fatalf("state = %q, want unknown", got.State)
	}
	if got.Networks == nil {
		t.Fatal("networks is nil, want empty slice")
	}
	if got.ExposedPorts == nil {
		t.Fatal("exposedPorts is nil, want empty slice")
	}
	if got.Labels == nil {
		t.Fatal("labels is nil, want empty map")
	}
}
