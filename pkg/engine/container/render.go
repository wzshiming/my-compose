package container

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/wzshiming/my-compose/pkg/engine/internal/render"
	"github.com/wzshiming/my-compose/pkg/plan"
)

const noEquivalent = "container has no equivalent"

func (dialect) Render(step plan.Step) ([]string, []plan.Warning, error) {
	var b render.Builder
	switch s := step.(type) {
	case plan.CreateNetwork:
		renderNetwork(&b, s.Network)
	case plan.CreateVolume:
		renderVolume(&b, s.Volume)
	case plan.BuildImage:
		if s.Build.Tag == "" {
			return nil, nil, render.Empty(s, "Tag")
		}
		if s.Build.Context == "" {
			return nil, nil, render.Empty(s, "Context")
		}
		renderBuild(&b, s.Build)
	case plan.RunContainer:
		if s.Container.Name == "" {
			return nil, nil, render.Empty(s, "Name")
		}
		if s.Container.Image == "" {
			return nil, nil, render.Empty(s, "Image")
		}
		renderRun(&b, s.Container)
	case plan.StartContainers:
		if len(s.Names) == 0 {
			return nil, nil, render.Empty(s, "Names")
		}
		if len(s.Names) > 1 {
			return nil, nil, fmt.Errorf("%T: container start accepts one name, got %d", s, len(s.Names))
		}
		b.Args("start", s.Names[0])
	case plan.StopContainers:
		if len(s.Names) == 0 {
			return nil, nil, render.Empty(s, "Names")
		}
		b.Args("stop")
		if s.Timeout != nil {
			b.Args("--time", render.Seconds(*s.Timeout))
		}
		b.Args(s.Names...)
	case plan.RemoveContainers:
		if len(s.Names) == 0 {
			return nil, nil, render.Empty(s, "Names")
		}
		b.Args("delete", "--force")
		b.Args(s.Names...)
		if s.Volumes {
			for _, name := range s.Names {
				b.SetScope("containers." + name)
				b.Ignored("volumes", "container keeps anonymous volumes; run container volume prune")
			}
		}
	case plan.RemoveNetwork:
		if s.Name == "" {
			return nil, nil, render.Empty(s, "Name")
		}
		b.Args("network", "delete", s.Name)
	case plan.RemoveVolume:
		if s.Name == "" {
			return nil, nil, render.Empty(s, "Name")
		}
		b.Args("volume", "delete", s.Name)
	case plan.WaitContainer:
		if s.Name == "" {
			return nil, nil, render.Empty(s, "Name")
		}
		if s.Condition == plan.ConditionHealthy {
			return nil, nil, fmt.Errorf("%T: container has no healthchecks to wait on for %s", s, s.Name)
		}
		return nil, nil, nil
	case plan.ListContainers:
		b.Args("list", "--all", "--format", "json")
	case plan.ListNetworks:
		b.Args("network", "list", "--format", "json")
	case plan.ListVolumes:
		b.Args("volume", "list", "--format", "json")
	case plan.ListImages:
		b.Args("image", "list", "--format", "json")
	case plan.InspectContainer:
		if s.Name == "" {
			return nil, nil, render.Empty(s, "Name")
		}
		b.Args("inspect", s.Name)
	case plan.Logs:
		if s.Name == "" {
			return nil, nil, render.Empty(s, "Name")
		}
		b.Args("logs")
		if s.Follow {
			b.Args("--follow")
		}
		if s.Tail != "" && s.Tail != "all" {
			b.Args("-n", s.Tail)
		}
		b.Args(s.Name)
	default:
		return nil, nil, render.Unsupported(step)
	}
	argv, ws := b.Finish()
	return argv, ws, nil
}

func renderNetwork(b *render.Builder, n plan.NetworkSpec) {
	b.SetScope("networks." + n.Name)
	b.Args("network", "create")
	if n.Internal {
		b.Args("--internal")
	}
	b.KV("--label", n.Labels)
	var v4, v6 string
	for _, p := range n.IPAM {
		family := &v4
		if strings.Contains(p.Subnet, ":") {
			family = &v6
		}
		switch {
		case p.Subnet == "":
		case *family != "":
			b.Ignored("ipam.config", "container accepts one subnet per address family")
		default:
			*family = p.Subnet
		}
		b.IgnoredIf(p.Gateway != "", "ipam.config.gateway", noEquivalent)
		b.IgnoredIf(p.IPRange != "", "ipam.config.ip_range", noEquivalent)
		b.IgnoredIf(len(p.AuxAddresses) > 0, "ipam.config.aux_addresses", noEquivalent)
	}
	if v4 != "" {
		b.Args("--subnet", v4)
	}
	if v6 != "" {
		b.Args("--subnet-v6", v6)
	}
	b.IgnoredIf(n.IPAMDriver != "" && n.IPAMDriver != "default", "ipam.driver", noEquivalent)
	b.IgnoredIf(len(n.IPAMOptions) > 0, "ipam.options", noEquivalent)
	b.IgnoredIf(n.Driver != "" && n.Driver != "bridge", "driver", noEquivalent)
	b.IgnoredIf(n.Attachable, "attachable", noEquivalent)
	b.IgnoredIf(n.EnableIPv4 != nil && !*n.EnableIPv4, "enable_ipv4", noEquivalent)
	b.IgnoredIf(len(n.DriverOpts) > 0, "driver_opts", "container network options are plugin-specific")
	b.IgnoredIf(n.EnableIPv6 != nil && *n.EnableIPv6 && v6 == "", "enable_ipv6", "container needs an ipv6 subnet in ipam.config")
	b.Args(n.Name)
}

func renderVolume(b *render.Builder, v plan.VolumeSpec) {
	b.SetScope("volumes." + v.Name)
	b.Args("volume", "create")
	b.KV("--label", v.Labels)
	for _, o := range v.DriverOpts {
		if o.Key == "size" || o.Key == "journal" {
			b.Args("--opt", o.Key+"="+o.Value)
		} else {
			b.Ignored("driver_opts."+o.Key, noEquivalent)
		}
	}
	b.IgnoredIf(v.Driver != "" && v.Driver != "local", "driver", noEquivalent)
	b.Args(v.Name)
}

func renderBuild(b *render.Builder, s plan.BuildSpec) {
	b.SetScope("services." + s.Service)
	b.Args("build")
	if s.Dockerfile != "" {
		b.Args("--file", s.Dockerfile)
	}
	b.KV("--build-arg", s.Args)
	if s.Target != "" {
		b.Args("--target", s.Target)
	}
	b.KV("--label", s.Labels)
	if s.NoCache {
		b.Args("--no-cache")
	}
	if s.Pull {
		b.Args("--pull")
	}
	b.Repeat("--platform", s.Platforms)
	for _, ssh := range s.SSH {
		if ssh != "default" {
			b.Ignored("build.ssh", "container supports only --ssh default")
		}
	}
	if slices.Contains(s.SSH, "default") {
		b.Args("--ssh", "default")
	}
	for _, sec := range s.Secrets {
		spec := "id=" + sec.ID
		if sec.Source != "" {
			spec += ",src=" + sec.Source
		}
		b.Args("--secret", spec)
	}
	b.Args("--tag", s.Tag)
	b.Repeat("--tag", s.Tags)
	b.IgnoredIf(len(s.CacheFrom) > 0, "build.cache_from", noEquivalent)
	b.IgnoredIf(len(s.CacheTo) > 0, "build.cache_to", noEquivalent)
	b.IgnoredIf(len(s.ExtraHosts) > 0, "build.extra_hosts", noEquivalent)
	b.IgnoredIf(s.Network != "", "build.network", noEquivalent)
	b.IgnoredIf(s.ShmSize != 0, "build.shm_size", noEquivalent)
	b.IgnoredIf(len(s.AdditionalContexts) > 0, "build.additional_contexts", noEquivalent)
	b.IgnoredIf(len(s.Ulimits) > 0, "build.ulimits", noEquivalent)
	b.IgnoredIf(s.Isolation != "", "build.isolation", noEquivalent)
	b.IgnoredIf(s.Privileged, "build.privileged", noEquivalent)
	b.Args(s.Context)
}

func renderRun(b *render.Builder, c plan.ContainerSpec) {
	b.SetScope("services." + c.Service)
	b.Args("run", "--detach", "--name", c.Name)
	if c.Platform != "" {
		b.Args("--platform", c.Platform)
	}
	b.KV("--env", c.Env)
	for _, l := range c.Labels {
		if strings.Contains(l.Value, "=") {
			b.Ignored("labels", l.Key+`: container rejects label values containing "="`)
			continue
		}
		b.Args("--label", l.Key+"="+l.Value)
	}
	if c.User != "" {
		b.Args("--user", c.User)
	}
	if c.WorkingDir != "" {
		b.Args("--workdir", c.WorkingDir)
	}
	if c.Tty {
		b.Args("--tty")
	}
	if c.StdinOpen {
		b.Args("--interactive")
	}
	if c.Init != nil && *c.Init {
		b.Args("--init")
	}
	if c.ReadOnly {
		b.Args("--read-only")
	}
	b.Repeat("--cap-add", c.CapAdd)
	b.Repeat("--cap-drop", c.CapDrop)
	for _, u := range c.Ulimits {
		b.Args("--ulimit", u.Name+"="+strconv.Itoa(u.Soft)+":"+strconv.Itoa(u.Hard))
	}
	b.Repeat("--dns", c.DNS)
	b.Repeat("--dns-search", c.DNSSearch)
	b.Repeat("--dns-option", c.DNSOpts)
	b.Repeat("--tmpfs", c.Tmpfs)
	if c.ShmSize > 0 {
		b.Args("--shm-size", mebibytes(c.ShmSize))
	}

	// args trail the image: the entrypoint tail, then the command.
	var args []string
	switch {
	case c.Entrypoint == nil:
	case len(c.Entrypoint) == 0:
		b.Ignored("entrypoint", "container cannot clear the image entrypoint")
	default:
		b.Args("--entrypoint", c.Entrypoint[0])
		args = append(args, c.Entrypoint[1:]...)
	}
	if c.Command != nil && len(c.Command) == 0 && c.Entrypoint == nil {
		b.Ignored("command", "container cannot clear the image command")
	}
	args = append(args, c.Command...)

	if c.Memory > 0 {
		m := mebibytes(c.Memory)
		if c.Memory%(1<<20) != 0 {
			b.Degraded("memory", m, "container allocates memory in whole mebibytes")
		}
		b.Args("--memory", m)
	}
	if c.CPUs > 0 {
		n := strconv.FormatInt(int64(math.Ceil(c.CPUs)), 10)
		if c.CPUs != math.Ceil(c.CPUs) {
			b.Degraded("cpus", n, "container allocates whole cpus")
		}
		b.Args("--cpus", n)
	}

	for _, p := range c.Ports {
		target := strconv.FormatUint(uint64(p.Target), 10)
		port := target + "/" + cmp.Or(p.Protocol, "tcp")
		switch {
		case p.Protocol != "" && p.Protocol != "tcp" && p.Protocol != "udp":
			b.Ignored("ports", port+": container publishes only tcp and udp")
			continue
		case p.Published == "":
			b.Ignored("ports", port+" has no host port; container requires one")
			continue
		}
		b.IgnoredIf(p.Mode != "" && p.Mode != "ingress", "ports.mode", noEquivalent)
		published, _, isRange := strings.Cut(p.Published, "-")
		if isRange {
			b.Degraded("ports", published, "container maps one host port per container port")
		}
		spec := published + ":" + target
		if p.HostIP != "" {
			spec = p.HostIP + ":" + spec
		}
		if p.Protocol != "" {
			spec += "/" + p.Protocol
		}
		b.Args("--publish", spec)
	}

	for _, m := range c.Mounts {
		if spec, ok := renderMount(b, m); ok {
			b.Args("--mount", spec)
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
		b.Args("--network", spec)
		key := "networks." + n.Key + "."
		b.IgnoredIf(n.IPv4 != "", key+"ipv4_address", noEquivalent)
		b.IgnoredIf(n.IPv6 != "", key+"ipv6_address", noEquivalent)
		b.IgnoredIf(len(n.LinkLocalIPs) > 0, key+"link_local_ips", noEquivalent)
		b.IgnoredIf(len(n.DriverOpts) > 0, key+"driver_opts", noEquivalent)
		b.IgnoredIf(n.Priority != 0, key+"priority", noEquivalent)
		b.IgnoredIf(n.GwPriority != 0, key+"gw_priority", noEquivalent)
		b.IgnoredIf(n.InterfaceName != "", key+"interface_name", noEquivalent)
	}
	b.IgnoredIf(mac != "", "mac_address", noEquivalent)
	b.IgnoredIf(c.NetworkMode != "", "network_mode", "container has no network modes; the default network is used")
	b.IgnoredIf(c.PullPolicy != "" && c.PullPolicy != "missing", "pull_policy", "container only pulls missing images")
	b.IgnoredIf(c.Healthcheck != nil && !c.Healthcheck.Disable, "healthcheck", "container has no healthchecks")
	b.IgnoredIf(c.Hostname != "", "hostname", noEquivalent)
	b.IgnoredIf(c.DomainName != "", "domainname", noEquivalent)
	b.IgnoredIf(len(c.ExtraHosts) > 0, "extra_hosts", noEquivalent)
	b.IgnoredIf(len(c.Links) > 0, "links", noEquivalent)
	b.IgnoredIf(len(c.Expose) > 0, "expose", noEquivalent)
	b.IgnoredIf(c.Restart != "" && c.Restart != "no", "restart", noEquivalent)
	b.IgnoredIf(c.StopSignal != "", "stop_signal", noEquivalent)
	b.IgnoredIf(c.Logging != nil, "logging", noEquivalent)
	b.IgnoredIf(len(c.Annotations) > 0, "annotations", noEquivalent)
	b.IgnoredIf(c.Privileged, "privileged", noEquivalent)
	b.IgnoredIf(len(c.SecurityOpt) > 0, "security_opt", noEquivalent)
	b.IgnoredIf(len(c.GroupAdd) > 0, "group_add", noEquivalent)
	b.IgnoredIf(len(c.DeviceCgroupRules) > 0, "device_cgroup_rules", noEquivalent)
	b.IgnoredIf(len(c.Devices) > 0, "devices", noEquivalent)
	b.IgnoredIf(len(c.Gpus) > 0, "gpus", noEquivalent)
	b.IgnoredIf(len(c.Sysctls) > 0, "sysctls", noEquivalent)
	b.IgnoredIf(c.Pid != "", "pid", noEquivalent)
	b.IgnoredIf(c.Ipc != "", "ipc", noEquivalent)
	b.IgnoredIf(c.Uts != "", "uts", noEquivalent)
	b.IgnoredIf(c.Userns != "", "userns_mode", noEquivalent)
	b.IgnoredIf(c.CgroupParent != "", "cgroup_parent", noEquivalent)
	b.IgnoredIf(c.Cgroup != "", "cgroup", noEquivalent)
	b.IgnoredIf(c.Runtime != "", "runtime", noEquivalent)
	b.IgnoredIf(c.Isolation != "", "isolation", noEquivalent)
	b.IgnoredIf(c.CPUShares != 0, "cpu_shares", noEquivalent)
	b.IgnoredIf(c.CPUSet != "", "cpuset", noEquivalent)
	b.IgnoredIf(c.CPUPeriod != 0, "cpu_period", noEquivalent)
	b.IgnoredIf(c.CPUQuota != 0, "cpu_quota", noEquivalent)
	b.IgnoredIf(c.CPURTPeriod != 0, "cpu_rt_period", noEquivalent)
	b.IgnoredIf(c.CPURTRuntime != 0, "cpu_rt_runtime", noEquivalent)
	b.IgnoredIf(c.CPUCount != 0, "cpu_count", noEquivalent)
	b.IgnoredIf(c.CPUPercent != 0, "cpu_percent", noEquivalent)
	b.IgnoredIf(c.MemoryReservation != 0, "mem_reservation", noEquivalent)
	b.IgnoredIf(c.MemorySwap != 0, "memswap_limit", noEquivalent)
	b.IgnoredIf(c.MemorySwappiness != 0, "mem_swappiness", noEquivalent)
	b.IgnoredIf(c.OomKillDisable, "oom_kill_disable", noEquivalent)
	b.IgnoredIf(c.OomScoreAdj != 0, "oom_score_adj", noEquivalent)
	b.IgnoredIf(c.PidsLimit != 0, "pids_limit", noEquivalent)
	b.IgnoredIf(c.Blkio != nil, "blkio_config", noEquivalent)
	b.IgnoredIf(len(c.VolumesFrom) > 0, "volumes_from", noEquivalent)
	b.IgnoredIf(c.VolumeDriver != "", "volume_driver", noEquivalent)
	b.IgnoredIf(len(c.StorageOpt) > 0, "storage_opt", noEquivalent)

	b.Args(c.Image)
	b.Args(args...)
}

// renderMount reports false when the mount type has no container equivalent.
func renderMount(b *render.Builder, m plan.Mount) (string, bool) {
	var opts []string
	switch m.Type {
	case "bind":
		opts = []string{"type=bind", "source=" + m.Source, "target=" + m.Target}
		if o := m.Bind; o != nil {
			b.IgnoredIf(o.Propagation != "", "volumes.bind.propagation", noEquivalent)
			b.IgnoredIf(o.SELinux != "", "volumes.bind.selinux", noEquivalent)
			b.IgnoredIf(o.Recursive != "", "volumes.bind.recursive", noEquivalent)
		}
	case "volume":
		opts = []string{"type=volume"}
		if m.Source != "" {
			opts = append(opts, "source="+m.Source)
		}
		opts = append(opts, "target="+m.Target)
		if v := m.Volume; v != nil {
			b.IgnoredIf(v.NoCopy, "volumes.volume.nocopy", noEquivalent)
			b.IgnoredIf(v.Subpath != "", "volumes.volume.subpath", noEquivalent)
			b.IgnoredIf(len(v.Labels) > 0, "volumes.volume.labels", noEquivalent)
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
		b.Ignored("volumes", "container has no "+m.Type+" mounts")
		return "", false
	}
	if m.ReadOnly {
		opts = append(opts, "readonly")
	}
	b.IgnoredIf(m.Consistency != "", "volumes.consistency", noEquivalent)
	return strings.Join(opts, ","), true
}

// mebibytes rounds n bytes up to whole MiB in the <n>M form container accepts.
func mebibytes(n int64) string {
	return strconv.FormatInt((n+(1<<20-1))>>20, 10) + "M"
}
