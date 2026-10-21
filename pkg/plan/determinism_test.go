package plan

import (
	"cmp"
	"reflect"
	"slices"
	"testing"
)

const richYAML = `
services:
  web:
    build: .
    image: example/web
    depends_on:
      db:
        condition: service_healthy
      cache:
        condition: service_started
    environment:
      ZED: "26"
      ALPHA: "1"
      MIDDLE: "13"
    labels:
      z.label: "1"
      a.label: "2"
      m.label: "3"
    sysctls:
      net.ipv4.tcp_syncookies: 0
      net.core.somaxconn: 1024
    ulimits:
      nproc: 65535
      nofile:
        soft: 20000
        hard: 40000
      core: 0
    extra_hosts:
      - zeta=10.0.0.3
      - alpha=10.0.0.1
      - mid=10.0.0.2
    networks:
      nc:
        priority: 100
      na: {}
      nb: {}
    volumes:
      - data:/data
      - logs:/logs
    secrets: [tok]
    configs: [cfg]
    ports: ["8080:80", "8443:443"]
  db:
    image: postgres
    scale: 2
    healthcheck:
      test: ["CMD", "pg_isready"]
    volumes: ["data:/var/lib/postgresql/data"]
  cache:
    image: redis
    networks: [nb, na]
  worker:
    image: alpine
    depends_on: [db, cache]
  sidecar:
    image: alpine
    network_mode: service:cache
networks:
  na: {name: na}
  nb: {name: nb}
  nc: {name: nc}
volumes:
  data: {}
  logs: {}
secrets:
  tok: {file: ./tok.txt}
configs:
  cfg: {file: ./cfg.txt}
`

func TestAC4Determinism(t *testing.T) {
	plans := func() (*Plan, []Warning, *Plan) {
		p := loadProject(t, richYAML)
		up, ws := mustUp(t, p, State{}, healthCaps, UpOptions{})
		return up, ws, mustDown(t, loadProject(t, richYAML), State{}, DownOptions{Volumes: true})
	}
	up0, ws0, down0 := plans()
	if len(up0.Steps) == 0 || len(down0.Steps) == 0 {
		t.Fatalf("Up steps = %v, Down steps = %v; want both non-empty", describePlan(up0), describePlan(down0))
	}
	for i := 1; i < 100; i++ {
		up, ws, down := plans()
		if !reflect.DeepEqual(up, up0) {
			t.Fatalf("Up run %d = %v, first run %v", i, describePlan(up), describePlan(up0))
		}
		if !slices.Equal(ws, ws0) {
			t.Fatalf("Up run %d warnings = %v, first run %v", i, ws, ws0)
		}
		if !reflect.DeepEqual(down, down0) {
			t.Fatalf("Down run %d = %v, first run %v", i, describePlan(down), describePlan(down0))
		}
	}
}

func TestAC4SortedFields(t *testing.T) {
	plan, _ := mustUp(t, loadProject(t, richYAML), State{}, healthCaps, UpOptions{})
	byKey := func(a, b KV) int { return cmp.Compare(a.Key, b.Key) }
	kvs := []struct {
		name string
		get  func(ContainerSpec) []KV
		min  int
	}{
		{"Env", func(c ContainerSpec) []KV { return c.Env }, 3},
		{"Labels", func(c ContainerSpec) []KV { return c.Labels }, 3},
		{"Sysctls", func(c ContainerSpec) []KV { return c.Sysctls }, 2},
	}
	for _, tt := range kvs {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.get(runSpecOf(t, plan, "p-web-1"))
			if len(got) < tt.min || !slices.IsSortedFunc(got, byKey) {
				t.Errorf("%s = %v, want at least %d entries sorted by Key", tt.name, got, tt.min)
			}
		})
	}
	t.Run("Ulimits", func(t *testing.T) {
		got := runSpecOf(t, plan, "p-web-1").Ulimits
		if len(got) != 3 || !slices.IsSortedFunc(got, func(a, b Ulimit) int { return cmp.Compare(a.Name, b.Name) }) {
			t.Errorf("Ulimits = %v, want 3 entries sorted by Name", got)
		}
	})
	t.Run("ExtraHosts", func(t *testing.T) {
		got := runSpecOf(t, plan, "p-web-1").ExtraHosts
		if want := []string{"alpha:10.0.0.1", "mid:10.0.0.2", "zeta:10.0.0.3"}; !slices.Equal(got, want) {
			t.Errorf("ExtraHosts = %v, want %v", got, want)
		}
	})
	t.Run("Networks", func(t *testing.T) {
		var got []string
		for _, n := range runSpecOf(t, plan, "p-web-1").Networks {
			got = append(got, n.Name)
		}
		if want := []string{"nc", "na", "nb"}; !slices.Equal(got, want) {
			t.Errorf("Networks = %v, want %v (priority, then name)", got, want)
		}
	})
}
