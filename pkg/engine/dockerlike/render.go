package dockerlike

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wzshiming/my-compose/pkg/plan"
)

// renderer accumulates one command's argv and warnings; cmd keys the variant's missing table.
type renderer struct {
	v     *variant
	cmd   string
	scope string
	argv  []string
	ws    []plan.Warning
}

func (d dialect) Render(step plan.Step) ([]string, []plan.Warning, error) {
	r := &renderer{v: d.v}
	switch s := step.(type) {
	case plan.CreateNetwork:
		if s.Network.Name == "" {
			return nil, nil, empty(s, "Name")
		}
		r.network(s.Network)
	case plan.CreateVolume:
		if s.Volume.Name == "" {
			return nil, nil, empty(s, "Name")
		}
		r.volume(s.Volume)
	case plan.BuildImage:
		if s.Build.Tag == "" {
			return nil, nil, empty(s, "Tag")
		}
		if s.Build.Context == "" {
			return nil, nil, empty(s, "Context")
		}
		r.build(s.Build)
	case plan.RunContainer:
		if s.Container.Name == "" {
			return nil, nil, empty(s, "Name")
		}
		if s.Container.Image == "" {
			return nil, nil, empty(s, "Image")
		}
		r.run(s.Container)
	case plan.StartContainers:
		if len(s.Names) == 0 {
			return nil, nil, empty(s, "Names")
		}
		r.argv = append([]string{"start"}, s.Names...)
	case plan.StopContainers:
		if len(s.Names) == 0 {
			return nil, nil, empty(s, "Names")
		}
		r.argv = []string{"stop"}
		if s.Timeout != nil {
			r.argv = append(r.argv, "--time", seconds(*s.Timeout))
		}
		r.argv = append(r.argv, s.Names...)
	case plan.RemoveContainers:
		if len(s.Names) == 0 {
			return nil, nil, empty(s, "Names")
		}
		r.argv = []string{"rm", "-f"}
		if s.Volumes {
			r.argv = append(r.argv, "-v")
		}
		r.argv = append(r.argv, s.Names...)
	case plan.RemoveNetwork:
		if s.Name == "" {
			return nil, nil, empty(s, "Name")
		}
		r.argv = []string{"network", "rm", s.Name}
	case plan.RemoveVolume:
		if s.Name == "" {
			return nil, nil, empty(s, "Name")
		}
		r.argv = []string{"volume", "rm", s.Name}
	case plan.WaitContainer:
		if s.Condition == plan.ConditionHealthy && !d.Capabilities().Healthcheck {
			return nil, nil, fmt.Errorf("%s cannot wait for %s to be healthy without healthcheck support", d.v.name, s.Name)
		}
		return nil, nil, nil
	case plan.ListContainers:
		r.argv = []string{"ps", "-a"}
		if d.v.psNoTrunc {
			r.argv = append(r.argv, "--no-trunc")
		}
		if s.Project != "" {
			r.argv = append(r.argv, "--filter", "label="+plan.LabelProject+"="+s.Project)
		}
		r.argv = append(r.argv, "--format", d.v.listFormat)
	case plan.ListNetworks:
		r.argv = []string{"network", "ls", "--format", d.v.listFormat}
	case plan.ListVolumes:
		r.argv = []string{"volume", "ls", "--format", d.v.listFormat}
	case plan.ListImages:
		r.argv = []string{"image", "ls", "--format", d.v.listFormat}
	case plan.InspectContainer:
		if s.Name == "" {
			return nil, nil, empty(s, "Name")
		}
		r.argv = []string{"inspect", s.Name}
	case plan.Logs:
		if s.Name == "" {
			return nil, nil, empty(s, "Name")
		}
		r.argv = []string{"logs"}
		if s.Follow {
			r.argv = append(r.argv, "-f")
		}
		if s.Tail != "" && s.Tail != "all" {
			r.argv = append(r.argv, "--tail", s.Tail)
		}
		r.argv = append(r.argv, s.Name)
	default:
		return nil, nil, fmt.Errorf("unsupported step %T", step)
	}
	return r.argv, plan.NormalizeWarnings(r.ws), nil
}

func empty(step plan.Step, field string) error {
	return fmt.Errorf("%T: empty %s", step, field)
}

func (r *renderer) ignored(attr, reason string) {
	r.ws = append(r.ws, plan.Warning{Scope: r.scope, Attribute: attr, Message: "ignored (" + reason + ")"})
}

func (r *renderer) degraded(attr, value, reason string) {
	r.ws = append(r.ws, plan.Warning{Scope: r.scope, Attribute: attr, Message: "degraded to " + value + " (" + reason + ")"})
}

func (r *renderer) inexpressible(attr string, set bool) {
	if set {
		r.ignored(attr, "not expressible on the command line")
	}
}

// has reports whether the variant's cmd takes flag, warning on attr when it does not.
func (r *renderer) has(flag, attr string) bool {
	if slices.Contains(r.v.missing, r.cmd+" "+flag) {
		r.ignored(attr, r.v.name+" has no "+flag)
		return false
	}
	return true
}

func (r *renderer) on(flag, attr string, set bool) {
	if set && r.has(flag, attr) {
		r.argv = append(r.argv, flag)
	}
}

func (r *renderer) each(flag, attr string, values []string) {
	if len(values) == 0 || !r.has(flag, attr) {
		return
	}
	for _, v := range values {
		r.argv = append(r.argv, flag, v)
	}
}

func (r *renderer) str(flag, attr, value string) {
	r.each(flag, attr, nonEmpty(value))
}

func (r *renderer) num(flag, attr string, n int64) {
	if n != 0 {
		r.str(flag, attr, strconv.FormatInt(n, 10))
	}
}

func (r *renderer) kv(flag, attr string, kvs []plan.KV) {
	r.each(flag, attr, specs(kvs, func(kv plan.KV) string { return kv.Key + "=" + kv.Value }))
}

func (r *renderer) duration(flag, attr string, d time.Duration) {
	if d != 0 {
		r.str(flag, attr, d.String())
	}
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func specs[T any](items []T, spec func(T) string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = spec(item)
	}
	return out
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// seconds rounds d up to whole seconds.
func seconds(d time.Duration) string {
	return strconv.FormatInt(int64(math.Ceil(d.Seconds())), 10)
}

func (r *renderer) network(n plan.NetworkSpec) {
	r.cmd, r.scope = "network create", "networks."+n.Name
	r.argv = []string{"network", "create"}
	r.str("--driver", "driver", n.Driver)
	r.kv("--opt", "driver_opts", n.DriverOpts)
	r.str("--ipam-driver", "ipam.driver", n.IPAMDriver)
	for _, p := range n.IPAM {
		r.str("--subnet", "ipam.config.subnet", p.Subnet)
		r.str("--gateway", "ipam.config.gateway", p.Gateway)
		r.str("--ip-range", "ipam.config.ip_range", p.IPRange)
		r.kv("--aux-address", "ipam.config.aux_addresses", p.AuxAddresses)
	}
	r.kv("--ipam-opt", "ipam.options", n.IPAMOptions)
	r.on("--internal", "internal", n.Internal)
	r.on("--attachable", "attachable", n.Attachable)
	r.on("--ipv6", "enable_ipv6", n.EnableIPv6 != nil && *n.EnableIPv6)
	if n.EnableIPv4 != nil && !*n.EnableIPv4 && r.has("--ipv4", "enable_ipv4") {
		r.argv = append(r.argv, "--ipv4=false")
	}
	r.kv("--label", "labels", n.Labels)
	r.argv = append(r.argv, n.Name)
}

func (r *renderer) volume(v plan.VolumeSpec) {
	r.cmd, r.scope = "volume create", "volumes."+v.Name
	r.argv = []string{"volume", "create"}
	r.str("--driver", "driver", v.Driver)
	r.kv("--opt", "driver_opts", v.DriverOpts)
	r.kv("--label", "labels", v.Labels)
	r.argv = append(r.argv, v.Name)
}

func (r *renderer) build(b plan.BuildSpec) {
	r.cmd, r.scope = "build", "services."+b.Service
	r.argv = []string{"build", "-t", b.Tag}
	r.each("-t", "build.tags", b.Tags)
	r.str("-f", "build.dockerfile", b.Dockerfile)
	r.kv("--build-arg", "build.args", b.Args)
	r.str("--target", "build.target", b.Target)
	r.kv("--label", "build.labels", b.Labels)
	r.on("--no-cache", "build.no_cache", b.NoCache)
	r.on("--pull", "build.pull", b.Pull)
	r.str("--platform", "build.platforms", strings.Join(b.Platforms, ","))
	r.each("--cache-from", "build.cache_from", b.CacheFrom)
	r.each("--cache-to", "build.cache_to", b.CacheTo)
	r.each("--ssh", "build.ssh", b.SSH)
	r.each("--secret", "build.secrets", specs(b.Secrets, func(s plan.BuildSecret) string {
		if s.Source == "" {
			return "id=" + s.ID
		}
		return "id=" + s.ID + ",src=" + s.Source
	}))
	r.str("--network", "build.network", b.Network)
	r.num("--shm-size", "build.shm_size", b.ShmSize)
	r.each("--add-host", "build.extra_hosts", b.ExtraHosts)
	r.each("--ulimit", "build.ulimits", specs(b.Ulimits, ulimitSpec))
	r.str("--isolation", "build.isolation", b.Isolation)
	r.kv("--build-context", "build.additional_contexts", b.AdditionalContexts)
	if b.Privileged && r.has("--allow", "build.privileged") {
		r.argv = append(r.argv, "--allow", "security.insecure")
	}
	r.argv = append(r.argv, b.Context)
}

func (r *renderer) run(c plan.ContainerSpec) {
	r.cmd, r.scope = "run", "services."+c.Service
	r.argv = []string{"run", "-d", "--name", c.Name}
	r.str("--platform", "platform", c.Platform)
	if c.PullPolicy == "always" || c.PullPolicy == "never" {
		r.str("--pull", "pull_policy", c.PullPolicy)
	}
	r.kv("--label", "labels", c.Labels)
	r.kv("--annotation", "annotations", c.Annotations)
	var lead []string // entrypoint elements after the first precede the command
	switch {
	case c.Entrypoint == nil:
	case len(c.Entrypoint) == 0:
		r.argv = append(r.argv, "--entrypoint", "")
	default:
		r.str("--entrypoint", "entrypoint", c.Entrypoint[0])
		lead = c.Entrypoint[1:]
		if len(lead) > 0 && c.Command == nil {
			r.degraded("entrypoint", c.Entrypoint[0], "extra elements replace the image cmd")
		}
	}
	if c.Command != nil && len(c.Command) == 0 {
		r.ignored("command", "an empty command cannot clear the image cmd")
	}
	r.str("--workdir", "working_dir", c.WorkingDir)
	r.str("--user", "user", c.User)
	r.on("-t", "tty", c.Tty)
	r.on("-i", "stdin_open", c.StdinOpen)
	r.on("--init", "init", c.Init != nil && *c.Init)
	r.str("--stop-signal", "stop_signal", c.StopSignal)
	if c.StopTimeout != nil {
		r.str("--stop-timeout", "stop_grace_period", seconds(*c.StopTimeout))
	}
	r.kv("--env", "environment", c.Env)
	r.str("--restart", "restart", c.Restart)
	r.str("--hostname", "hostname", r.networks(c))
	r.str("--domainname", "domainname", c.DomainName)
	r.str("--mac-address", "mac_address", c.MacAddress)
	r.each("--dns", "dns", c.DNS)
	r.each("--dns-search", "dns_search", c.DNSSearch)
	r.each("--dns-option", "dns_opt", c.DNSOpts)
	r.each("--add-host", "extra_hosts", c.ExtraHosts)
	r.each("-p", "ports", specs(c.Ports, portSpec))
	r.each("--expose", "expose", c.Expose)
	r.each("--link", "links", c.Links)
	r.mounts(c.Mounts)
	r.each("--tmpfs", "tmpfs", c.Tmpfs)
	r.num("--shm-size", "shm_size", c.ShmSize)
	r.each("--volumes-from", "volumes_from", c.VolumesFrom)
	r.str("--volume-driver", "volume_driver", c.VolumeDriver)
	r.kv("--storage-opt", "storage_opt", c.StorageOpt)
	r.on("--read-only", "read_only", c.ReadOnly)
	r.on("--privileged", "privileged", c.Privileged)
	r.each("--cap-add", "cap_add", c.CapAdd)
	r.each("--cap-drop", "cap_drop", c.CapDrop)
	r.each("--security-opt", "security_opt", c.SecurityOpt)
	r.each("--group-add", "group_add", c.GroupAdd)
	r.each("--device-cgroup-rule", "device_cgroup_rules", c.DeviceCgroupRules)
	r.each("--device", "devices", specs(c.Devices, deviceSpec))
	r.each("--gpus", "gpus", specs(c.Gpus, gpuSpec))
	r.kv("--sysctl", "sysctls", c.Sysctls)
	r.each("--ulimit", "ulimits", specs(c.Ulimits, ulimitSpec))
	r.str("--pid", "pid", c.Pid)
	r.str("--ipc", "ipc", c.Ipc)
	r.str("--uts", "uts", c.Uts)
	r.str("--userns", "userns_mode", c.Userns)
	r.str("--cgroup-parent", "cgroup_parent", c.CgroupParent)
	r.str("--cgroupns", "cgroup", c.Cgroup)
	r.str("--runtime", "runtime", c.Runtime)
	r.str("--isolation", "isolation", c.Isolation)
	if cpus := c.CPUs; cpus != 0 {
		if r.v.integerCPUs && cpus != math.Ceil(cpus) {
			cpus = math.Ceil(cpus)
			r.degraded("cpus", ftoa(cpus), r.v.name+" takes whole cpus")
		}
		r.str("--cpus", "cpus", ftoa(cpus))
	}
	r.num("--cpu-shares", "cpu_shares", c.CPUShares)
	r.str("--cpuset-cpus", "cpuset", c.CPUSet)
	r.num("--cpu-period", "cpu_period", c.CPUPeriod)
	r.num("--cpu-quota", "cpu_quota", c.CPUQuota)
	r.num("--cpu-rt-period", "cpu_rt_period", c.CPURTPeriod)
	r.num("--cpu-rt-runtime", "cpu_rt_runtime", c.CPURTRuntime)
	r.num("--cpu-count", "cpu_count", c.CPUCount)
	if c.CPUPercent != 0 {
		r.str("--cpu-percent", "cpu_percent", ftoa(c.CPUPercent))
	}
	r.num("--memory", "mem_limit", c.Memory)
	r.num("--memory-reservation", "mem_reservation", c.MemoryReservation)
	r.num("--memory-swap", "memswap_limit", c.MemorySwap)
	r.num("--memory-swappiness", "mem_swappiness", c.MemorySwappiness)
	r.on("--oom-kill-disable", "oom_kill_disable", c.OomKillDisable)
	r.num("--oom-score-adj", "oom_score_adj", c.OomScoreAdj)
	r.num("--pids-limit", "pids_limit", c.PidsLimit)
	if b := c.Blkio; b != nil && r.has("--blkio-weight", "blkio_config") {
		r.num("--blkio-weight", "blkio_config.weight", int64(b.Weight))
		r.each("--blkio-weight-device", "blkio_config.weight_device", specs(b.WeightDevice, func(d plan.WeightDevice) string {
			return d.Path + ":" + strconv.FormatUint(uint64(d.Weight), 10)
		}))
		r.each("--device-read-bps", "blkio_config.device_read_bps", specs(b.DeviceReadBps, throttleSpec))
		r.each("--device-write-bps", "blkio_config.device_write_bps", specs(b.DeviceWriteBps, throttleSpec))
		r.each("--device-read-iops", "blkio_config.device_read_iops", specs(b.DeviceReadIOps, throttleSpec))
		r.each("--device-write-iops", "blkio_config.device_write_iops", specs(b.DeviceWriteIOps, throttleSpec))
	}
	r.healthcheck(c.Healthcheck)
	if l := c.Logging; l != nil && r.has("--log-driver", "logging") {
		r.str("--log-driver", "logging.driver", l.Driver)
		r.kv("--log-opt", "logging.options", l.Options)
	}
	r.argv = append(r.argv, c.Image)
	r.argv = append(r.argv, lead...)
	r.argv = append(r.argv, c.Command...)
}

// netOpt is a network attachment attribute: its flag on the first network and its key in --network name=... afterwards.
type netOpt struct {
	flag, key, attr string
	values          []string
}

// networks renders network_mode or the attachments and returns the hostname to use.
func (r *renderer) networks(c plan.ContainerSpec) string {
	hostname := c.Hostname
	if c.NetworkMode != "" {
		r.str("--network", "network_mode", c.NetworkMode)
		if len(c.Networks) > 0 {
			r.ignored("networks", "network_mode is set")
		}
		return hostname
	}
	var union []string // every alias once, for aliasAll
	for _, n := range c.Networks {
		for _, a := range n.Aliases {
			if !slices.Contains(union, a) {
				union = append(union, a)
			}
		}
	}
	for i, n := range c.Networks {
		attr := "networks." + n.Key + "."
		aliases := n.Aliases
		switch r.v.aliasMode {
		case aliasAll:
			aliases = nil
			if i == 0 {
				aliases = union
			}
		case aliasHostname:
			if c.Hostname == "" && slices.Contains(aliases, c.Service) {
				hostname = c.Service
				aliases = slices.DeleteFunc(slices.Clone(aliases), func(a string) bool { return a == c.Service })
			}
		}
		opts := []netOpt{
			{"--network-alias", "alias", "aliases", aliases},
			{"--ip", "ip", "ipv4_address", nonEmpty(n.IPv4)},
			{"--ip6", "ip6", "ipv6_address", nonEmpty(n.IPv6)},
			{"--mac-address", "mac-address", "mac_address", nonEmpty(n.MacAddress)},
			{"--link-local-ip", "link-local-ip", "link_local_ips", n.LinkLocalIPs},
		}
		network := n.Name
		if i == 0 {
			r.argv = append(r.argv, "--network", network)
			for _, o := range opts {
				r.each(o.flag, attr+o.attr, o.values)
			}
		} else {
			var kvs []string
			for _, o := range opts {
				switch {
				case len(o.values) == 0 || !r.has(o.flag, attr+o.attr):
				case !slices.Contains(r.v.networkKV, o.key):
					r.ignored(attr+o.attr, r.v.name+" takes "+o.flag+" for the first network only")
				default:
					for _, v := range o.values {
						kvs = append(kvs, o.key+"="+v)
					}
				}
			}
			if len(kvs) > 0 {
				network = "name=" + network + "," + strings.Join(kvs, ",")
			}
			r.argv = append(r.argv, "--network", network)
		}
		r.inexpressible(attr+"driver_opts", len(n.DriverOpts) > 0)
		r.inexpressible(attr+"gw_priority", n.GwPriority != 0)
		r.inexpressible(attr+"interface_name", n.InterfaceName != "")
	}
	return hostname
}

func (r *renderer) mounts(mounts []plan.Mount) {
	for _, m := range mounts {
		attr := "volumes." + m.Target + "."
		r.inexpressible(attr+"consistency", m.Consistency != "")
		if m.Type == "bind" {
			var opts []string
			if m.ReadOnly {
				opts = append(opts, "ro")
			}
			if b := m.Bind; b != nil {
				if b.SELinux != "" && r.has("--mount bind-selinux", attr+"bind.selinux") {
					opts = append(opts, b.SELinux)
				}
				if b.Propagation != "" && r.has("--mount bind-propagation", attr+"bind.propagation") {
					opts = append(opts, b.Propagation)
				}
				r.inexpressible(attr+"bind.recursive", b.Recursive != "" && b.Recursive != "enabled")
			}
			spec := m.Source + ":" + m.Target
			if len(opts) > 0 {
				spec += ":" + strings.Join(opts, ",")
			}
			r.argv = append(r.argv, "-v", spec)
			continue
		}
		opts := []string{"type=" + m.Type}
		if m.Source != "" {
			opts = append(opts, "source="+m.Source)
		}
		opts = append(opts, "target="+m.Target)
		if m.ReadOnly {
			opts = append(opts, "readonly")
		}
		if v := m.Volume; v != nil {
			if v.NoCopy && r.has("--mount volume-nocopy", attr+"volume.nocopy") {
				opts = append(opts, "volume-nocopy")
			}
			if v.Subpath != "" && r.has("--mount volume-subpath", attr+"volume.subpath") {
				opts = append(opts, "volume-subpath="+v.Subpath)
			}
			r.inexpressible(attr+"volume.labels", len(v.Labels) > 0)
		}
		if im := m.Image; im != nil && im.Subpath != "" && r.has("--mount image-subpath", attr+"image.subpath") {
			opts = append(opts, "image-subpath="+im.Subpath)
		}
		if t := m.Tmpfs; t != nil {
			if t.Size != 0 && r.has("--mount tmpfs-size", attr+"tmpfs.size") {
				opts = append(opts, "tmpfs-size="+strconv.FormatInt(t.Size, 10))
			}
			if t.Mode != 0 && r.has("--mount tmpfs-mode", attr+"tmpfs.mode") {
				opts = append(opts, "tmpfs-mode="+strconv.FormatUint(uint64(t.Mode), 8))
			}
		}
		r.argv = append(r.argv, "--mount", strings.Join(opts, ","))
	}
}

func (r *renderer) healthcheck(h *plan.Healthcheck) {
	if h == nil {
		return
	}
	if h.Disable || slices.Equal(h.Test, []string{"NONE"}) {
		r.argv = append(r.argv, "--no-healthcheck")
		return
	}
	r.str("--health-cmd", "healthcheck.test", healthCmd(h.Test))
	r.duration("--health-interval", "healthcheck.interval", h.Interval)
	r.duration("--health-timeout", "healthcheck.timeout", h.Timeout)
	r.duration("--health-start-period", "healthcheck.start_period", h.StartPeriod)
	r.duration("--health-start-interval", "healthcheck.start_interval", h.StartInterval)
	r.num("--health-retries", "healthcheck.retries", int64(h.Retries))
}

// healthCmd turns a compose test list into the shell line --health-cmd takes.
func healthCmd(test []string) string {
	if len(test) == 0 {
		return ""
	}
	switch test[0] {
	case "CMD-SHELL":
		return strings.Join(test[1:], " ")
	case "CMD":
		test = test[1:]
	}
	return strings.Join(specs(test, shellQuote), " ")
}

// shellQuote single-quotes w for POSIX sh when it holds whitespace or shell metacharacters.
func shellQuote(w string) string {
	if w != "" && !strings.ContainsAny(w, " \t\n'\"$`\\|&;<>()*?[]#~") {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}

func portSpec(p plan.Port) string {
	spec := strconv.FormatUint(uint64(p.Target), 10)
	if p.Protocol != "" {
		spec += "/" + p.Protocol
	}
	switch {
	case p.HostIP != "":
		spec = p.HostIP + ":" + p.Published + ":" + spec
	case p.Published != "":
		spec = p.Published + ":" + spec
	}
	return spec
}

func deviceSpec(d plan.Device) string {
	spec := d.Source
	if d.Target != "" {
		spec += ":" + d.Target
		if d.Permissions != "" {
			spec += ":" + d.Permissions
		}
	}
	return spec
}

func gpuSpec(g plan.DeviceRequest) string {
	var kvs []string
	if g.Driver != "" {
		kvs = append(kvs, "driver="+g.Driver)
	}
	switch {
	case len(g.IDs) > 0:
		kvs = append(kvs, csvKV("device", g.IDs))
	case g.Count > 0:
		kvs = append(kvs, "count="+strconv.FormatInt(g.Count, 10))
	default:
		kvs = append(kvs, "count=all")
	}
	if len(g.Capabilities) > 0 {
		kvs = append(kvs, csvKV("capabilities", g.Capabilities))
	}
	for _, o := range g.Options {
		kvs = append(kvs, o.Key+"="+o.Value)
	}
	return strings.Join(kvs, ",")
}

// csvKV double-quotes a multi-valued pair so the CSV parser keeps the values together.
func csvKV(key string, values []string) string {
	kv := key + "=" + strings.Join(values, ",")
	if len(values) > 1 {
		return `"` + kv + `"`
	}
	return kv
}

func ulimitSpec(u plan.Ulimit) string {
	spec := u.Name + "=" + strconv.Itoa(u.Soft)
	if u.Hard != 0 {
		spec += ":" + strconv.Itoa(u.Hard)
	}
	return spec
}

func throttleSpec(t plan.ThrottleDevice) string {
	return t.Path + ":" + strconv.FormatInt(t.Rate, 10)
}
