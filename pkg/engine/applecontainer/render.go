package applecontainer

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/wzshiming/my-compose/pkg/plan"
)

const noEquivalent = "container has no equivalent"

type warner struct {
	scope string
	ws    []plan.Warning
}

// attr pairs a compose attribute path with whether the spec sets it.
type attr struct {
	path string
	set  bool
}

func (w *warner) ignored(path, reason string) {
	w.ws = append(w.ws, plan.Warning{Scope: w.scope, Attribute: path, Message: "ignored (" + reason + ")"})
}

func (w *warner) degraded(path, value, reason string) {
	w.ws = append(w.ws, plan.Warning{Scope: w.scope, Attribute: path, Message: "degraded to " + value + " (" + reason + ")"})
}

func (w *warner) unsupported(attrs []attr) {
	for _, a := range attrs {
		if a.set {
			w.ignored(a.path, noEquivalent)
		}
	}
}

func missing(step plan.Step, field string) error {
	return fmt.Errorf("%T: empty %s", step, field)
}

func (dialect) Render(step plan.Step) ([]string, []plan.Warning, error) {
	var argv []string
	var ws []plan.Warning
	switch s := step.(type) {
	case plan.CreateNetwork:
		argv, ws = renderNetwork(s.Network)
	case plan.CreateVolume:
		argv, ws = renderVolume(s.Volume)
	case plan.BuildImage:
		if s.Build.Tag == "" {
			return nil, nil, missing(s, "Tag")
		}
		if s.Build.Context == "" {
			return nil, nil, missing(s, "Context")
		}
		argv, ws = renderBuild(s.Build)
	case plan.RunContainer:
		if s.Container.Name == "" {
			return nil, nil, missing(s, "Name")
		}
		if s.Container.Image == "" {
			return nil, nil, missing(s, "Image")
		}
		argv, ws = renderRun(s.Container)
	case plan.StartContainers:
		if len(s.Names) == 0 {
			return nil, nil, missing(s, "Names")
		}
		if len(s.Names) > 1 {
			return nil, nil, fmt.Errorf("%T: container start accepts one name, got %d", s, len(s.Names))
		}
		argv = []string{"start", s.Names[0]}
	case plan.StopContainers:
		if len(s.Names) == 0 {
			return nil, nil, missing(s, "Names")
		}
		argv = []string{"stop"}
		if s.Timeout != nil {
			argv = append(argv, "--time", strconv.FormatInt(int64(math.Ceil(s.Timeout.Seconds())), 10))
		}
		argv = append(argv, s.Names...)
	case plan.RemoveContainers:
		if len(s.Names) == 0 {
			return nil, nil, missing(s, "Names")
		}
		argv = append([]string{"delete", "--force"}, s.Names...)
		if s.Volumes {
			for _, name := range s.Names {
				ws = append(ws, plan.Warning{Scope: "containers." + name, Attribute: "volumes", Message: "ignored (container keeps anonymous volumes; run container volume prune)"})
			}
		}
	case plan.RemoveNetwork:
		if s.Name == "" {
			return nil, nil, missing(s, "Name")
		}
		argv = []string{"network", "delete", s.Name}
	case plan.RemoveVolume:
		if s.Name == "" {
			return nil, nil, missing(s, "Name")
		}
		argv = []string{"volume", "delete", s.Name}
	case plan.WaitContainer:
		if s.Name == "" {
			return nil, nil, missing(s, "Name")
		}
		if s.Condition == plan.ConditionHealthy {
			return nil, nil, fmt.Errorf("%T: container has no healthchecks to wait on for %s", s, s.Name)
		}
		return nil, nil, nil
	case plan.ListContainers:
		argv = []string{"list", "--all", "--format", "json"}
	case plan.ListNetworks:
		argv = []string{"network", "list", "--format", "json"}
	case plan.ListVolumes:
		argv = []string{"volume", "list", "--format", "json"}
	case plan.ListImages:
		argv = []string{"image", "list", "--format", "json"}
	case plan.InspectContainer:
		if s.Name == "" {
			return nil, nil, missing(s, "Name")
		}
		argv = []string{"inspect", s.Name}
	case plan.Logs:
		if s.Name == "" {
			return nil, nil, missing(s, "Name")
		}
		argv = []string{"logs"}
		if s.Follow {
			argv = append(argv, "--follow")
		}
		if s.Tail != "" && s.Tail != "all" {
			argv = append(argv, "-n", s.Tail)
		}
		argv = append(argv, s.Name)
	default:
		return nil, nil, fmt.Errorf("unsupported step %T", step)
	}
	return argv, plan.NormalizeWarnings(ws), nil
}

func renderNetwork(n plan.NetworkSpec) ([]string, []plan.Warning) {
	w := &warner{scope: "networks." + n.Name}
	argv := []string{"network", "create"}
	if n.Internal {
		argv = append(argv, "--internal")
	}
	argv = kvFlags(argv, "--label", n.Labels)
	var v4, v6 string
	for _, p := range n.IPAM {
		family := &v4
		if strings.Contains(p.Subnet, ":") {
			family = &v6
		}
		switch {
		case p.Subnet == "":
		case *family != "":
			w.ignored("ipam.config", "container accepts one subnet per address family")
		default:
			*family = p.Subnet
		}
		w.unsupported([]attr{
			{"ipam.config.gateway", p.Gateway != ""},
			{"ipam.config.ip_range", p.IPRange != ""},
			{"ipam.config.aux_addresses", len(p.AuxAddresses) > 0},
		})
	}
	if v4 != "" {
		argv = append(argv, "--subnet", v4)
	}
	if v6 != "" {
		argv = append(argv, "--subnet-v6", v6)
	}
	w.unsupported([]attr{
		{"ipam.driver", n.IPAMDriver != "" && n.IPAMDriver != "default"},
		{"ipam.options", len(n.IPAMOptions) > 0},
		{"driver", n.Driver != "" && n.Driver != "bridge"},
		{"attachable", n.Attachable},
		{"enable_ipv4", n.EnableIPv4 != nil && !*n.EnableIPv4},
	})
	if len(n.DriverOpts) > 0 {
		w.ignored("driver_opts", "container network options are plugin-specific")
	}
	if n.EnableIPv6 != nil && *n.EnableIPv6 && v6 == "" {
		w.ignored("enable_ipv6", "container needs an ipv6 subnet in ipam.config")
	}
	return append(argv, n.Name), w.ws
}

func renderVolume(v plan.VolumeSpec) ([]string, []plan.Warning) {
	w := &warner{scope: "volumes." + v.Name}
	argv := kvFlags([]string{"volume", "create"}, "--label", v.Labels)
	for _, o := range v.DriverOpts {
		if o.Key == "size" || o.Key == "journal" {
			argv = append(argv, "--opt", o.Key+"="+o.Value)
		} else {
			w.ignored("driver_opts."+o.Key, noEquivalent)
		}
	}
	if v.Driver != "" && v.Driver != "local" {
		w.ignored("driver", noEquivalent)
	}
	return append(argv, v.Name), w.ws
}

func renderBuild(b plan.BuildSpec) ([]string, []plan.Warning) {
	w := &warner{scope: "services." + b.Service}
	argv := []string{"build"}
	if b.Dockerfile != "" {
		argv = append(argv, "--file", b.Dockerfile)
	}
	argv = kvFlags(argv, "--build-arg", b.Args)
	if b.Target != "" {
		argv = append(argv, "--target", b.Target)
	}
	argv = kvFlags(argv, "--label", b.Labels)
	if b.NoCache {
		argv = append(argv, "--no-cache")
	}
	if b.Pull {
		argv = append(argv, "--pull")
	}
	argv = repeatFlag(argv, "--platform", b.Platforms)
	for _, s := range b.SSH {
		if s != "default" {
			w.ignored("build.ssh", "container supports only --ssh default")
		}
	}
	if slices.Contains(b.SSH, "default") {
		argv = append(argv, "--ssh", "default")
	}
	for _, s := range b.Secrets {
		spec := "id=" + s.ID
		if s.Source != "" {
			spec += ",src=" + s.Source
		}
		argv = append(argv, "--secret", spec)
	}
	argv = append(argv, "--tag", b.Tag)
	argv = repeatFlag(argv, "--tag", b.Tags)
	w.unsupported([]attr{
		{"build.cache_from", len(b.CacheFrom) > 0},
		{"build.cache_to", len(b.CacheTo) > 0},
		{"build.extra_hosts", len(b.ExtraHosts) > 0},
		{"build.network", b.Network != ""},
		{"build.shm_size", b.ShmSize != 0},
		{"build.additional_contexts", len(b.AdditionalContexts) > 0},
		{"build.ulimits", len(b.Ulimits) > 0},
		{"build.isolation", b.Isolation != ""},
		{"build.privileged", b.Privileged},
	})
	return append(argv, b.Context), w.ws
}

func renderRun(c plan.ContainerSpec) ([]string, []plan.Warning) {
	w := &warner{scope: "services." + c.Service}
	argv := []string{"run", "--detach", "--name", c.Name}
	if c.Platform != "" {
		argv = append(argv, "--platform", c.Platform)
	}
	argv = kvFlags(argv, "--env", c.Env)
	for _, l := range c.Labels {
		if strings.Contains(l.Value, "=") {
			w.ignored("labels", l.Key+`: container rejects label values containing "="`)
			continue
		}
		argv = append(argv, "--label", l.Key+"="+l.Value)
	}
	if c.User != "" {
		argv = append(argv, "--user", c.User)
	}
	if c.WorkingDir != "" {
		argv = append(argv, "--workdir", c.WorkingDir)
	}
	if c.Tty {
		argv = append(argv, "--tty")
	}
	if c.StdinOpen {
		argv = append(argv, "--interactive")
	}
	if c.Init != nil && *c.Init {
		argv = append(argv, "--init")
	}
	if c.ReadOnly {
		argv = append(argv, "--read-only")
	}
	argv = repeatFlag(argv, "--cap-add", c.CapAdd)
	argv = repeatFlag(argv, "--cap-drop", c.CapDrop)
	for _, u := range c.Ulimits {
		argv = append(argv, "--ulimit", u.Name+"="+strconv.Itoa(u.Soft)+":"+strconv.Itoa(u.Hard))
	}
	argv = repeatFlag(argv, "--dns", c.DNS)
	argv = repeatFlag(argv, "--dns-search", c.DNSSearch)
	argv = repeatFlag(argv, "--dns-option", c.DNSOpts)
	argv = repeatFlag(argv, "--tmpfs", c.Tmpfs)
	if c.ShmSize > 0 {
		argv = append(argv, "--shm-size", mebibytes(c.ShmSize))
	}

	var args []string
	switch {
	case c.Entrypoint == nil:
	case len(c.Entrypoint) == 0:
		w.ignored("entrypoint", "container cannot clear the image entrypoint")
	default:
		argv = append(argv, "--entrypoint", c.Entrypoint[0])
		args = append(args, c.Entrypoint[1:]...)
	}
	if c.Command != nil && len(c.Command) == 0 && c.Entrypoint == nil {
		w.ignored("command", "container cannot clear the image command")
	}
	args = append(args, c.Command...)

	if c.Memory > 0 {
		m := mebibytes(c.Memory)
		if c.Memory%(1<<20) != 0 {
			w.degraded("memory", m, "container allocates memory in whole mebibytes")
		}
		argv = append(argv, "--memory", m)
	}
	if c.CPUs > 0 {
		n := strconv.FormatInt(int64(math.Ceil(c.CPUs)), 10)
		if c.CPUs != math.Ceil(c.CPUs) {
			w.degraded("cpus", n, "container allocates whole cpus")
		}
		argv = append(argv, "--cpus", n)
	}

	for _, p := range c.Ports {
		target := strconv.FormatUint(uint64(p.Target), 10)
		port := target + "/" + cmp.Or(p.Protocol, "tcp")
		switch {
		case p.Protocol != "" && p.Protocol != "tcp" && p.Protocol != "udp":
			w.ignored("ports", port+": container publishes only tcp and udp")
			continue
		case p.Published == "":
			w.ignored("ports", port+" has no host port; container requires one")
			continue
		}
		if p.Mode != "" && p.Mode != "ingress" {
			w.ignored("ports.mode", noEquivalent)
		}
		published, _, isRange := strings.Cut(p.Published, "-")
		if isRange {
			w.degraded("ports", published, "container maps one host port per container port")
		}
		spec := published + ":" + target
		if p.HostIP != "" {
			spec = p.HostIP + ":" + spec
		}
		if p.Protocol != "" {
			spec += "/" + p.Protocol
		}
		argv = append(argv, "--publish", spec)
	}

	for _, m := range c.Mounts {
		if spec, ok := renderMount(m, w); ok {
			argv = append(argv, "--mount", spec)
		}
	}

	mac := c.MacAddress
	for _, n := range c.Networks {
		spec := n.Name
		for _, a := range n.Aliases {
			spec += ",alias=" + a
		}
		m := n.MacAddress
		if m == "" {
			m, mac = mac, ""
		}
		if m != "" {
			spec += ",mac=" + m
		}
		argv = append(argv, "--network", spec)
		key := "networks." + n.Key + "."
		w.unsupported([]attr{
			{key + "ipv4_address", n.IPv4 != ""},
			{key + "ipv6_address", n.IPv6 != ""},
			{key + "link_local_ips", len(n.LinkLocalIPs) > 0},
			{key + "driver_opts", len(n.DriverOpts) > 0},
			{key + "priority", n.Priority != 0},
			{key + "gw_priority", n.GwPriority != 0},
			{key + "interface_name", n.InterfaceName != ""},
		})
	}
	if mac != "" {
		w.ignored("mac_address", noEquivalent)
	}
	if c.NetworkMode != "" {
		w.ignored("network_mode", "container has no network modes; the default network is used")
	}
	if c.PullPolicy != "" && c.PullPolicy != "missing" {
		w.ignored("pull_policy", "container only pulls missing images")
	}
	if c.Healthcheck != nil && !c.Healthcheck.Disable {
		w.ignored("healthcheck", "container has no healthchecks")
	}
	w.unsupported([]attr{
		{"hostname", c.Hostname != ""},
		{"domainname", c.DomainName != ""},
		{"extra_hosts", len(c.ExtraHosts) > 0},
		{"links", len(c.Links) > 0},
		{"expose", len(c.Expose) > 0},
		{"restart", c.Restart != "" && c.Restart != "no"},
		{"stop_signal", c.StopSignal != ""},
		{"logging", c.Logging != nil},
		{"annotations", len(c.Annotations) > 0},
		{"privileged", c.Privileged},
		{"security_opt", len(c.SecurityOpt) > 0},
		{"group_add", len(c.GroupAdd) > 0},
		{"device_cgroup_rules", len(c.DeviceCgroupRules) > 0},
		{"devices", len(c.Devices) > 0},
		{"gpus", len(c.Gpus) > 0},
		{"sysctls", len(c.Sysctls) > 0},
		{"pid", c.Pid != ""},
		{"ipc", c.Ipc != ""},
		{"uts", c.Uts != ""},
		{"userns_mode", c.Userns != ""},
		{"cgroup_parent", c.CgroupParent != ""},
		{"cgroup", c.Cgroup != ""},
		{"runtime", c.Runtime != ""},
		{"isolation", c.Isolation != ""},
		{"cpu_shares", c.CPUShares != 0},
		{"cpuset", c.CPUSet != ""},
		{"cpu_period", c.CPUPeriod != 0},
		{"cpu_quota", c.CPUQuota != 0},
		{"cpu_rt_period", c.CPURTPeriod != 0},
		{"cpu_rt_runtime", c.CPURTRuntime != 0},
		{"cpu_count", c.CPUCount != 0},
		{"cpu_percent", c.CPUPercent != 0},
		{"mem_reservation", c.MemoryReservation != 0},
		{"memswap_limit", c.MemorySwap != 0},
		{"mem_swappiness", c.MemorySwappiness != 0},
		{"oom_kill_disable", c.OomKillDisable},
		{"oom_score_adj", c.OomScoreAdj != 0},
		{"pids_limit", c.PidsLimit != 0},
		{"blkio_config", c.Blkio != nil},
		{"volumes_from", len(c.VolumesFrom) > 0},
		{"volume_driver", c.VolumeDriver != ""},
		{"storage_opt", len(c.StorageOpt) > 0},
	})

	argv = append(argv, c.Image)
	return append(argv, args...), w.ws
}

// renderMount reports false when the mount type has no container equivalent.
func renderMount(m plan.Mount, w *warner) (string, bool) {
	var opts []string
	switch m.Type {
	case "bind":
		opts = []string{"type=bind", "source=" + m.Source, "target=" + m.Target}
		if b := m.Bind; b != nil {
			w.unsupported([]attr{
				{"volumes.bind.propagation", b.Propagation != ""},
				{"volumes.bind.selinux", b.SELinux != ""},
				{"volumes.bind.recursive", b.Recursive != ""},
			})
		}
	case "volume":
		opts = []string{"type=volume"}
		if m.Source != "" {
			opts = append(opts, "source="+m.Source)
		}
		opts = append(opts, "target="+m.Target)
		if v := m.Volume; v != nil {
			w.unsupported([]attr{
				{"volumes.volume.nocopy", v.NoCopy},
				{"volumes.volume.subpath", v.Subpath != ""},
				{"volumes.volume.labels", len(v.Labels) > 0},
			})
		}
	case "tmpfs":
		opts = []string{"type=tmpfs", "target=" + m.Target}
		if t := m.Tmpfs; t != nil {
			if t.Size > 0 {
				opts = append(opts, "size="+mebibytes(t.Size))
			}
			if t.Mode != 0 {
				opts = append(opts, "mode="+strconv.FormatUint(uint64(t.Mode), 8))
			}
		}
	default:
		w.ignored("volumes", "container has no "+m.Type+" mounts")
		return "", false
	}
	if m.ReadOnly {
		opts = append(opts, "readonly")
	}
	if m.Consistency != "" {
		w.ignored("volumes.consistency", noEquivalent)
	}
	return strings.Join(opts, ","), true
}

func kvFlags(argv []string, flag string, kvs []plan.KV) []string {
	for _, kv := range kvs {
		argv = append(argv, flag, kv.Key+"="+kv.Value)
	}
	return argv
}

func repeatFlag(argv []string, flag string, values []string) []string {
	for _, v := range values {
		argv = append(argv, flag, v)
	}
	return argv
}

// mebibytes rounds n bytes up to whole MiB in the <n>M form container accepts.
func mebibytes(n int64) string {
	return strconv.FormatInt((n+(1<<20-1))>>20, 10) + "M"
}
