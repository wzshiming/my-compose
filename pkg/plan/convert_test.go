package plan

import (
	"cmp"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
)

func TestAC1Naming(t *testing.T) {
	t.Run("replicas are numbered from 1", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, `{services: {web: {image: nginx, scale: 3}}}`), State{}, healthCaps, UpOptions{})
		if got, want := sortedCopy(stepArgs(plan, "RunContainer")), []string{"p-web-1", "p-web-2", "p-web-3"}; !slices.Equal(got, want) {
			t.Errorf("RunContainer names = %v, want %v", got, want)
		}
	})
	t.Run("container_name replaces the generated name", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, `{services: {web: {image: nginx, container_name: my-web}}}`), State{}, healthCaps, UpOptions{})
		if got, want := stepArgs(plan, "RunContainer"), []string{"my-web"}; !slices.Equal(got, want) {
			t.Errorf("RunContainer names = %v, want %v", got, want)
		}
	})
	t.Run("container_name with two replicas is an error", func(t *testing.T) {
		p := loadProject(t, `{services: {web: {image: nginx, container_name: my-web}}}`)
		editService(p, "web", func(s *types.ServiceConfig) { s.SetScale(2) })
		if _, _, err := Up(p, State{}, healthCaps, UpOptions{}); err == nil {
			t.Error("Up succeeded, want an error for container_name with 2 replicas")
		}
	})
	t.Run("networks are named by name or project_key", func(t *testing.T) {
		p := loadProject(t, `{services: {web: {image: nginx, networks: [front, shared]}}, networks: {front: {}, shared: {name: shared-net}}}`)
		plan, _ := mustUp(t, p, State{}, healthCaps, UpOptions{})
		if got, want := sortedCopy(stepArgs(plan, "CreateNetwork")), []string{"p_front", "shared-net"}; !slices.Equal(got, want) {
			t.Errorf("CreateNetwork names = %v, want %v", got, want)
		}
	})
	t.Run("volumes are named by name or project_key", func(t *testing.T) {
		p := loadProject(t, `{services: {web: {image: nginx, volumes: ["data:/data", "keep:/keep"]}}, volumes: {data: {}, keep: {name: kept-vol}}}`)
		plan, _ := mustUp(t, p, State{}, healthCaps, UpOptions{})
		if got, want := sortedCopy(stepArgs(plan, "CreateVolume")), []string{"kept-vol", "p_data"}; !slices.Equal(got, want) {
			t.Errorf("CreateVolume names = %v, want %v", got, want)
		}
	})
	t.Run("build without image is tagged project-service", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, `{services: {web: {build: .}}}`), State{}, healthCaps, UpOptions{})
		if got, want := stepArgs(plan, "BuildImage"), []string{"p-web"}; !slices.Equal(got, want) {
			t.Errorf("BuildImage tags = %v, want %v", got, want)
		}
		if got := runSpecOf(t, plan, "p-web-1").Image; got != "p-web" {
			t.Errorf("Image = %q, want %q", got, "p-web")
		}
	})
}

func TestAC2Labels(t *testing.T) {
	const yaml = `
services:
  web:
    image: nginx
    networks: [front]
    volumes: ["data:/data"]
    labels:
      com.docker.compose.project: hijack
      com.docker.compose.oneoff: "True"
      app: demo
networks:
  front: {}
volumes:
  data: {}
`
	files := []string{filepath.Join(tstProjDir, "compose.yaml"), filepath.Join(tstProjDir, "compose.prod.yaml")}
	up := func(t *testing.T) *Plan {
		p := loadProject(t, yaml)
		p.ComposeFiles = files
		plan, _ := mustUp(t, p, State{}, healthCaps, UpOptions{})
		return plan
	}
	t.Run("container gets compose labels over user labels", func(t *testing.T) {
		labels := kvsToMap(t, runSpecOf(t, up(t), "p-web-1").Labels)
		want := map[string]string{
			LabelProject:         "p",
			LabelService:         "web",
			LabelContainerNumber: "1",
			LabelOneoff:          "False",
			LabelWorkingDir:      tstProjDir,
			LabelConfigFiles:     strings.Join(files, ","),
			"app":                "demo",
		}
		for k, v := range want {
			if got, ok := labels[k]; !ok || got != v {
				t.Errorf("label %s = %q, want %q", k, got, v)
			}
		}
	})
	t.Run("network gets project and network labels", func(t *testing.T) {
		plan := up(t)
		for _, s := range stepsOf[CreateNetwork](plan) {
			if s.Network.Name != "p_front" {
				continue
			}
			labels := kvsToMap(t, s.Network.Labels)
			if labels[LabelProject] != "p" || labels[LabelNetwork] != "front" {
				t.Errorf("network labels = %v, want %s=p and %s=front", s.Network.Labels, LabelProject, LabelNetwork)
			}
			return
		}
		t.Errorf("no CreateNetwork p_front in %v", describePlan(plan))
	})
	t.Run("volume gets project and volume labels", func(t *testing.T) {
		plan := up(t)
		for _, s := range stepsOf[CreateVolume](plan) {
			if s.Volume.Name != "p_data" {
				continue
			}
			labels := kvsToMap(t, s.Volume.Labels)
			if labels[LabelProject] != "p" || labels[LabelVolume] != "data" {
				t.Errorf("volume labels = %v, want %s=p and %s=data", s.Volume.Labels, LabelProject, LabelVolume)
			}
			return
		}
		t.Errorf("no CreateVolume p_data in %v", describePlan(plan))
	})
}

func TestAC8ServiceReferences(t *testing.T) {
	const yaml = `
services:
  x:
    image: alpine
  shared:
    image: alpine
    network_mode: service:x
    pid: service:x
    ipc: service:x
  linked:
    image: alpine
    volumes_from: ["x:ro"]
    links: ["x:db"]
    external_links: ["outside:ext"]
`
	spec := func(t *testing.T, name string) ContainerSpec {
		plan, _ := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
		return runSpecOf(t, plan, name)
	}
	tests := []struct {
		name      string
		container string
		got       func(ContainerSpec) any
		want      any
	}{
		{"network_mode", "p-shared-1", func(c ContainerSpec) any { return c.NetworkMode }, "container:p-x-1"},
		{"pid", "p-shared-1", func(c ContainerSpec) any { return c.Pid }, "container:p-x-1"},
		{"ipc", "p-shared-1", func(c ContainerSpec) any { return c.Ipc }, "container:p-x-1"},
		{"volumes_from keeps the access mode", "p-linked-1", func(c ContainerSpec) any { return c.VolumesFrom }, []string{"p-x-1:ro"}},
		{"links then external_links", "p-linked-1", func(c ContainerSpec) any { return c.Links }, []string{"p-x-1:db", "outside:ext"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got(spec(t, tt.container)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

type testMount struct {
	Type, Source, Target string
	ReadOnly             bool
}

func testMounts(ms []Mount) []testMount {
	out := []testMount{}
	for _, m := range ms {
		out = append(out, testMount{m.Type, m.Source, m.Target, m.ReadOnly})
	}
	return out
}

type testResources struct {
	CPUs                      float64
	Memory, MemoryReservation int64
}

type testHealth struct {
	Test     []string
	Interval time.Duration
	Retries  int
}

func TestContainerSpecConversion(t *testing.T) {
	commands := func(c ContainerSpec) any { return [2][]string{c.Command, c.Entrypoint} }
	restart := func(c ContainerSpec) any { return c.Restart }
	resources := func(c ContainerSpec) any { return testResources{c.CPUs, c.Memory, c.MemoryReservation} }
	logging := func(c ContainerSpec) any { return c.Logging }
	tests := []struct {
		name string
		yaml string
		edit func(*types.Project)
		got  func(ContainerSpec) any
		want any
	}{
		{"unset command and entrypoint keep the image defaults", `{services: {web: {image: nginx}}}`, nil,
			commands, [2][]string{nil, nil}},
		{"empty command and entrypoint clear the image defaults", `{services: {web: {image: nginx, command: [], entrypoint: []}}}`, nil,
			commands, [2][]string{{}, {}}},
		{"environment sorted without unset variables", `{services: {web: {image: nginx, environment: [B=2, A=1, UNSET, EMPTY=]}}}`, nil,
			func(c ContainerSpec) any { return c.Env }, []KV{{"A", "1"}, {"B", "2"}, {"EMPTY", ""}}},
		{"stop_grace_period", `{services: {web: {image: nginx, stop_grace_period: 1m30s}}}`, nil,
			func(c ContainerSpec) any { return c.StopTimeout }, ptrTo(90 * time.Second)},
		{"restart", `{services: {web: {image: nginx, restart: unless-stopped}}}`, nil,
			restart, "unless-stopped"},
		{"restart_policy any", `{services: {web: {image: nginx, deploy: {restart_policy: {condition: any}}}}}`, nil,
			restart, "always"},
		{"restart_policy on-failure with max_attempts", `{services: {web: {image: nginx, deploy: {restart_policy: {condition: on-failure, max_attempts: 3}}}}}`, nil,
			restart, "on-failure:3"},
		{"restart_policy none", `{services: {web: {image: nginx, deploy: {restart_policy: {condition: none}}}}}`, nil,
			restart, "no"},
		{"network_mode not naming a service", `{services: {web: {image: nginx, network_mode: host}}}`, nil,
			func(c ContainerSpec) any { return c.NetworkMode }, "host"},
		{"aliases followed by the service name", `{services: {web: {image: nginx, networks: {front: {aliases: [www, site]}}}}, networks: {front: {}}}`, nil,
			func(c ContainerSpec) any {
				if len(c.Networks) != 1 {
					return c.Networks
				}
				return c.Networks[0].Aliases
			}, []string{"www", "site", "web"}},
		{"extra_hosts as sorted host:ip", `{services: {web: {image: nginx, extra_hosts: ["zeta=10.0.0.3", "alpha=10.0.0.1"]}}}`, nil,
			func(c ContainerSpec) any { return c.ExtraHosts }, []string{"alpha:10.0.0.1", "zeta:10.0.0.3"}},
		{"volume mounts use engine volume names", `{services: {web: {image: nginx, volumes: ["data:/data", "/anon", "./src:/src:ro"]}}, volumes: {data: {}}}`, nil,
			func(c ContainerSpec) any { return testMounts(c.Mounts) },
			[]testMount{{"volume", "p_data", "/data", false}, {"volume", "", "/anon", false}, {"bind", filepath.Join(tstProjDir, "src"), "/src", true}}},
		{"file secrets and configs are read-only binds", `{services: {web: {image: nginx, secrets: [tok], configs: [cfg]}}, secrets: {tok: {file: ./tok.txt}}, configs: {cfg: {file: ./cfg.txt}}}`, nil,
			func(c ContainerSpec) any {
				ms := testMounts(c.Mounts)
				slices.SortFunc(ms, func(a, b testMount) int { return cmp.Compare(a.Target, b.Target) })
				return ms
			},
			[]testMount{{"bind", filepath.Join(tstProjDir, "cfg.txt"), "/cfg", true}, {"bind", filepath.Join(tstProjDir, "tok.txt"), "/run/secrets/tok", true}}},
		{"ulimits sorted, single value sets soft and hard", `{services: {web: {image: nginx, ulimits: {nproc: 65535, nofile: {soft: 1024, hard: 2048}}}}}`, nil,
			func(c ContainerSpec) any { return c.Ulimits }, []Ulimit{{"nofile", 1024, 2048}, {"nproc", 65535, 65535}}},
		{"cpus and memory from service attributes", `{services: {web: {image: nginx, cpus: 1.5, mem_limit: 512m, mem_reservation: 128m}}}`, nil,
			resources, testResources{1.5, 512 << 20, 128 << 20}},
		{"cpus and memory from deploy resources", `{services: {web: {image: nginx, deploy: {resources: {limits: {cpus: "0.5", memory: 256m}, reservations: {memory: 64m}}}}}}`, nil,
			resources, testResources{0.5, 256 << 20, 64 << 20}},
		{"healthcheck test kept as loaded", `{services: {web: {image: nginx, healthcheck: {test: "curl -f http://localhost", interval: 10s, retries: 3}}}}`, nil,
			func(c ContainerSpec) any {
				if c.Healthcheck == nil {
					return nil
				}
				return testHealth{c.Healthcheck.Test, c.Healthcheck.Interval, c.Healthcheck.Retries}
			}, testHealth{[]string{"CMD-SHELL", "curl -f http://localhost"}, 10 * time.Second, 3}},
		{"logging", `{services: {web: {image: nginx, logging: {driver: json-file, options: {max-size: 10m, max-file: "3"}}}}}`, nil,
			logging, &Logging{Driver: "json-file", Options: []KV{{"max-file", "3"}, {"max-size", "10m"}}}},
		{"logging from log_driver and log_opt", `{services: {web: {image: nginx}}}`,
			func(p *types.Project) {
				editService(p, "web", func(s *types.ServiceConfig) {
					s.LogDriver = "syslog"
					s.LogOpt = map[string]string{"tag": "web"}
				})
			},
			logging, &Logging{Driver: "syslog", Options: []KV{{"tag", "web"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := loadProject(t, tt.yaml)
			if tt.edit != nil {
				tt.edit(p)
			}
			plan, _ := mustUp(t, p, State{}, healthCaps, UpOptions{})
			if got := tt.got(runSpecOf(t, plan, "p-web-1")); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPullPolicy(t *testing.T) {
	tests := []struct {
		name, yaml, want string
		degraded         bool
	}{
		{"unset", `{services: {web: {image: nginx}}}`, "missing", false},
		{"always", `{services: {web: {image: nginx, pull_policy: always}}}`, "always", false},
		{"never", `{services: {web: {image: nginx, pull_policy: never}}}`, "never", false},
		{"if_not_present", `{services: {web: {image: nginx, pull_policy: if_not_present}}}`, "missing", false},
		{"missing", `{services: {web: {image: nginx, pull_policy: missing}}}`, "missing", false},
		{"build", `{services: {web: {image: nginx, build: ., pull_policy: build}}}`, "missing", false},
		{"refresh", `{services: {web: {image: nginx, pull_policy: refresh}}}`, "always", true},
		{"daily", `{services: {web: {image: nginx, pull_policy: daily}}}`, "always", true},
		{"weekly", `{services: {web: {image: nginx, pull_policy: weekly}}}`, "always", true},
		{"every_12h", `{services: {web: {image: nginx, pull_policy: every_12h}}}`, "always", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, ws := mustUp(t, loadProject(t, tt.yaml), State{}, healthCaps, UpOptions{})
			if got := runSpecOf(t, plan, "p-web-1").PullPolicy; got != tt.want {
				t.Errorf("PullPolicy = %q, want %q", got, tt.want)
			}
			if tt.degraded {
				expectWarning(t, ws, "services.web", "pull_policy", "degraded to always (")
			}
		})
	}
}

func TestIgnoredAttributeWarnings(t *testing.T) {
	const tok = `, secrets: {tok: {file: ./tok.txt}}}`
	tests := []struct{ yaml, scope, attribute string }{
		{`{services: {web: {image: nginx, develop: {watch: [{path: ./src, action: rebuild}]}}}}`, "services.web", "develop"},
		{`{services: {web: {image: nginx, use_api_socket: true}}}`, "services.web", "use_api_socket"},
		{`{services: {web: {image: nginx, attach: false}}}`, "services.web", "attach"},
		{`{services: {web: {image: nginx, credential_spec: {file: spec.json}}}}`, "services.web", "credential_spec"},
		{`{services: {web: {image: nginx, pre_start: [{command: [echo, init]}]}}}`, "services.web", "pre_start"},
		{`{services: {web: {image: nginx, post_start: [{command: [echo, hi]}]}}}`, "services.web", "post_start"},
		{`{services: {web: {image: nginx, pre_stop: [{command: [echo, bye]}]}}}`, "services.web", "pre_stop"},
		{`{services: {web: {image: nginx, models: [m]}}, models: {m: {model: ai/smollm2}}}`, "services.web", "models"},
		{`{services: {web: {image: nginx, models: [m]}}, models: {m: {model: ai/smollm2}}}`, "models.m", "models"},
		{`{services: {web: {image: nginx, deploy: {mode: global}}}}`, "services.web", "deploy.mode"},
		{`{services: {web: {image: nginx, deploy: {update_config: {parallelism: 2}}}}}`, "services.web", "deploy.update_config"},
		{`{services: {web: {image: nginx, deploy: {rollback_config: {parallelism: 2}}}}}`, "services.web", "deploy.rollback_config"},
		{`{services: {web: {image: nginx, deploy: {endpoint_mode: dnsrr}}}}`, "services.web", "deploy.endpoint_mode"},
		{`{services: {web: {image: nginx, deploy: {labels: {tier: front}}}}}`, "services.web", "deploy.labels"},
		{`{services: {web: {image: nginx, deploy: {resources: {reservations: {cpus: "0.5"}}}}}}`, "services.web", "deploy.resources.reservations.cpus"},
		{`{services: {web: {image: nginx, deploy: {resources: {reservations: {generic_resources: [{discrete_resource_spec: {kind: gpu, value: 1}}]}}}}}}`, "services.web", "deploy.resources.reservations.generic_resources"},
		{`{services: {web: {build: {context: ., dockerfile_inline: "FROM alpine"}}}}`, "services.web", "build.dockerfile_inline"},
		{`{services: {web: {build: {context: ., provenance: true}}}}`, "services.web", "build.provenance"},
		{`{services: {web: {build: {context: ., sbom: true}}}}`, "services.web", "build.sbom"},
		{`{services: {web: {build: {context: ., entitlements: [network.host]}}}}`, "services.web", "build.entitlements"},
		{`{services: {web: {image: nginx, secrets: [{source: tok, uid: "1000"}]}}` + tok, "services.web", "secrets.tok.uid"},
		{`{services: {web: {image: nginx, secrets: [{source: tok, gid: "1000"}]}}` + tok, "services.web", "secrets.tok.gid"},
		{`{services: {web: {image: nginx, secrets: [{source: tok, mode: 0440}]}}` + tok, "services.web", "secrets.tok.mode"},
	}
	for _, tt := range tests {
		t.Run(tt.scope+" "+tt.attribute, func(t *testing.T) {
			_, ws := mustUp(t, loadProject(t, tt.yaml), State{}, healthCaps, UpOptions{})
			expectWarning(t, ws, tt.scope, tt.attribute, "ignored (")
		})
	}
}

func TestAC12Warnings(t *testing.T) {
	const yaml = `
services:
  web:
    image: nginx
    pull_policy: daily
    secrets: [token]
    deploy:
      placement:
        constraints: ["node.role==manager"]
    depends_on:
      db:
        condition: service_healthy
  worker:
    image: alpine
    secrets: [token]
  db:
    image: postgres
    healthcheck:
      test: ["CMD", "pg_isready"]
secrets:
  token:
    environment: TOKEN
jobs:
  migrate:
    image: alpine
    triggers:
      manual: true
`
	format := regexp.MustCompile(`^[a-z]+\.\S+: \S+: (ignored|degraded to \S+) \(.+\)$`)
	up := func(t *testing.T) []Warning {
		t.Helper()
		_, ws := mustUp(t, loadProject(t, yaml), State{}, Capabilities{}, UpOptions{})
		if len(ws) == 0 {
			t.Fatal("Up returned no warnings")
		}
		return ws
	}
	t.Run("every warning matches the format", func(t *testing.T) {
		for _, w := range up(t) {
			if !format.MatchString(w.String()) {
				t.Errorf("warning %q does not match %s", w.String(), format)
			}
		}
	})
	t.Run("warnings are sorted and deduplicated", func(t *testing.T) {
		if ws := up(t); !slices.Equal(ws, NormalizeWarnings(ws)) {
			t.Errorf("warnings = %v, want them normalized: %v", ws, NormalizeWarnings(ws))
		}
	})
	t.Run("secret from environment", func(t *testing.T) {
		expectWarning(t, up(t), "secrets.token", "environment", "ignored (")
	})
	t.Run("top-level jobs", func(t *testing.T) {
		expectWarning(t, up(t), "jobs.migrate", "jobs", "ignored (")
	})
	t.Run("deploy.placement", func(t *testing.T) {
		expectWarning(t, up(t), "services.web", "deploy.placement", "ignored (")
	})
}

func TestG1NetworkAttachmentOptions(t *testing.T) {
	const yaml = `{services: {web: {image: nginx, networks: {front: {gw_priority: 10, interface_name: eth9}}}}, networks: {front: {}}}`
	plan, _ := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
	nets := runSpecOf(t, plan, "p-web-1").Networks
	if len(nets) != 1 {
		t.Fatalf("Networks = %+v, want one attachment", nets)
	}
	if nets[0].GwPriority != 10 || nets[0].InterfaceName != "eth9" {
		t.Errorf("GwPriority, InterfaceName = %d, %q; want 10, %q", nets[0].GwPriority, nets[0].InterfaceName, "eth9")
	}
}

func TestG1VolumeMountLabels(t *testing.T) {
	const yaml = `{services: {web: {image: nginx, volumes: [{type: volume, source: data, target: /data, volume: {labels: {b: "2", a: "1"}}}]}}, volumes: {data: {}}}`
	plan, _ := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
	mounts := runSpecOf(t, plan, "p-web-1").Mounts
	if len(mounts) != 1 || mounts[0].Volume == nil {
		t.Fatalf("Mounts = %+v, want one volume mount with volume options", mounts)
	}
	if got, want := mounts[0].Volume.Labels, []KV{{"a", "1"}, {"b", "2"}}; !slices.Equal(got, want) {
		t.Errorf("Volume.Labels = %v, want %v", got, want)
	}
}

func TestG1ExternalSecretWarning(t *testing.T) {
	const yaml = `{services: {web: {image: nginx, secrets: [tok], configs: [cfg]}}, secrets: {tok: {external: true}}, configs: {cfg: {external: true}}}`
	plan, ws := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
	expectWarning(t, ws, "secrets.tok", "external", "ignored (")
	expectWarning(t, ws, "configs.cfg", "external", "ignored (")
	if mounts := runSpecOf(t, plan, "p-web-1").Mounts; len(mounts) != 0 {
		t.Errorf("Mounts = %+v, want none for external secrets and configs", mounts)
	}
}

func tstBuildOf(t *testing.T, p *Plan, tag string) BuildSpec {
	t.Helper()
	for _, b := range stepsOf[BuildImage](p) {
		if b.Build.Tag == tag {
			return b.Build
		}
	}
	t.Fatalf("no BuildImage %s in %v", tag, describePlan(p))
	return BuildSpec{}
}

func TestS5BuildService(t *testing.T) {
	plan, _ := mustUp(t, loadProject(t, `{services: {api: {build: ./api}, web: {build: ./web}}}`), State{}, healthCaps, UpOptions{})
	for _, tt := range []struct{ tag, service string }{{"p-api", "api"}, {"p-web", "web"}} {
		if got := tstBuildOf(t, plan, tt.tag).Service; got != tt.service {
			t.Errorf("BuildImage %s: Build.Service = %q, want %q", tt.tag, got, tt.service)
		}
	}
}

func TestS5NetworkKey(t *testing.T) {
	const yaml = `{services: {web: {image: nginx, networks: [front, back]}}, networks: {front: {}, back: {name: back-net}}}`
	plan, _ := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
	var got []string
	for _, n := range runSpecOf(t, plan, "p-web-1").Networks {
		got = append(got, n.Key+"="+n.Name)
	}
	if want := []string{"back=back-net", "front=p_front"}; !slices.Equal(sortedCopy(got), want) {
		t.Errorf("attachments as Key=Name = %v, want %v", got, want)
	}
}

func TestS5DockerfilePath(t *testing.T) {
	srv := tstHostAbs("/srv/Dockerfile")
	tests := []struct{ name, build, want string }{
		{"relative dockerfile is joined to a local context", `{context: ., dockerfile: build/Dockerfile.dev}`, filepath.Join(tstProjDir, "build", "Dockerfile.dev")},
		{"absolute dockerfile is kept", `{context: ., dockerfile: '` + srv + `'}`, srv},
		{"relative dockerfile is kept for a url context", `{context: "https://github.com/x/y.git", dockerfile: build/Dockerfile.dev}`, "build/Dockerfile.dev"},
		{"relative dockerfile is kept for a git ssh context", `{context: "git@github.com:x/y.git", dockerfile: build/Dockerfile.dev}`, "build/Dockerfile.dev"},
		{"relative dockerfile is kept for a github shorthand context", `{context: github.com/x/y, dockerfile: build/Dockerfile.dev}`, "build/Dockerfile.dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, _ := mustUp(t, loadProject(t, `{services: {web: {build: `+tt.build+`}}}`), State{}, healthCaps, UpOptions{})
			if b := tstBuildOf(t, plan, "p-web"); b.Dockerfile != tt.want {
				t.Errorf("Build.Dockerfile = %q (Context %q), want %q", b.Dockerfile, b.Context, tt.want)
			}
		})
	}
}
