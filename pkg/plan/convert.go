package plan

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
)

func serviceContainerName(p *types.Project, key string, n int) string {
	if name := p.Services[key].ContainerName; name != "" {
		return name
	}
	return ContainerName(p.Name, key, n)
}

func replicaNames(p *types.Project, key string) ([]string, error) {
	s := p.Services[key]
	scale := s.GetScale()
	if s.ContainerName != "" && scale > 1 {
		return nil, fmt.Errorf("service %q: container_name %q cannot name %d replicas", key, s.ContainerName, scale)
	}
	var names []string
	for n := 1; n <= scale; n++ {
		names = append(names, serviceContainerName(p, key, n))
	}
	return names, nil
}

func resolveServiceRef(p *types.Project, ref string) string {
	if svc, ok := strings.CutPrefix(ref, types.ServicePrefix); ok {
		return types.ContainerPrefix + serviceContainerName(p, svc, 1)
	}
	return ref
}

func toContainerSpec(p *types.Project, key string, s types.ServiceConfig, n int, image, pullPolicy string) ContainerSpec {
	spec := ContainerSpec{
		Service:    key,
		Number:     n,
		Name:       serviceContainerName(p, key, n),
		Image:      image,
		Platform:   s.Platform,
		PullPolicy: pullPolicy,
		Labels: mergeLabels(s.Labels, map[string]string{
			LabelProject:         p.Name,
			LabelService:         key,
			LabelContainerNumber: strconv.Itoa(n),
			LabelOneoff:          "False",
			LabelWorkingDir:      p.WorkingDir,
			LabelConfigFiles:     strings.Join(p.ComposeFiles, ","),
		}),
		Annotations: sortedKV(s.Annotations),

		Command:     s.Command,
		Entrypoint:  s.Entrypoint,
		WorkingDir:  s.WorkingDir,
		User:        s.User,
		Tty:         s.Tty,
		StdinOpen:   s.StdinOpen,
		Init:        s.Init,
		StopSignal:  s.StopSignal,
		StopTimeout: stopTimeout(s.StopGracePeriod),
		Env:         sortedKV(s.Environment.ToMapping()),
		Restart:     s.Restart,

		NetworkMode: resolveServiceRef(p, s.NetworkMode),
		Hostname:    s.Hostname,
		DomainName:  s.DomainName,
		MacAddress:  s.MacAddress,
		DNS:         s.DNS,
		DNSSearch:   s.DNSSearch,
		DNSOpts:     s.DNSOpts,
		ExtraHosts:  sortedHosts(s.ExtraHosts),
		Expose:      s.Expose,

		Tmpfs:        s.Tmpfs,
		ShmSize:      int64(s.ShmSize),
		VolumeDriver: s.VolumeDriver,
		StorageOpt:   sortedKV(s.StorageOpt),
		ReadOnly:     s.ReadOnly,

		Privileged:        s.Privileged,
		CapAdd:            s.CapAdd,
		CapDrop:           s.CapDrop,
		SecurityOpt:       s.SecurityOpt,
		GroupAdd:          s.GroupAdd,
		DeviceCgroupRules: s.DeviceCgroupRules,
		Sysctls:           sortedKV(s.Sysctls),
		Ulimits:           ulimitList(s.Ulimits),
		Pid:               resolveServiceRef(p, s.Pid),
		Ipc:               resolveServiceRef(p, s.Ipc),
		Uts:               s.Uts,
		Userns:            s.UserNSMode,
		CgroupParent:      s.CgroupParent,
		Cgroup:            s.Cgroup,
		Runtime:           s.Runtime,
		Isolation:         s.Isolation,

		CPUs:              widen(s.CPUS),
		CPUShares:         s.CPUShares,
		CPUSet:            s.CPUSet,
		CPUPeriod:         s.CPUPeriod,
		CPUQuota:          s.CPUQuota,
		CPURTPeriod:       s.CPURTPeriod,
		CPURTRuntime:      s.CPURTRuntime,
		CPUCount:          s.CPUCount,
		CPUPercent:        widen(s.CPUPercent),
		Memory:            int64(s.MemLimit),
		MemoryReservation: int64(s.MemReservation),
		MemorySwap:        int64(s.MemSwapLimit),
		MemorySwappiness:  int64(s.MemSwappiness),
		OomKillDisable:    s.OomKillDisable,
		OomScoreAdj:       s.OomScoreAdj,
		PidsLimit:         s.PidsLimit,
	}

	for _, net := range s.NetworksByPriority() {
		cfg := s.Networks[net]
		if cfg == nil {
			cfg = &types.ServiceNetworkConfig{}
		}
		spec.Networks = append(spec.Networks, NetworkAttachment{
			Key:           net,
			Name:          NetworkName(p.Name, net, p.Networks[net]),
			Aliases:       append(slices.Clone(cfg.Aliases), key),
			IPv4:          cfg.Ipv4Address,
			IPv6:          cfg.Ipv6Address,
			MacAddress:    cfg.MacAddress,
			LinkLocalIPs:  cfg.LinkLocalIPs,
			DriverOpts:    sortedKV(cfg.DriverOpts),
			Priority:      cfg.Priority,
			GwPriority:    cfg.GatewayPriority,
			InterfaceName: cfg.InterfaceName,
		})
	}
	for _, port := range s.Ports {
		spec.Ports = append(spec.Ports, Port{
			Name:        port.Name,
			Mode:        port.Mode,
			HostIP:      port.HostIP,
			Published:   port.Published,
			Target:      port.Target,
			Protocol:    port.Protocol,
			AppProtocol: port.AppProtocol,
		})
	}
	for _, link := range s.Links {
		svc, alias, ok := strings.Cut(link, ":")
		if !ok {
			alias = svc
		}
		spec.Links = append(spec.Links, serviceContainerName(p, svc, 1)+":"+alias)
	}
	spec.Links = append(spec.Links, s.ExternalLinks...)

	for _, v := range s.Volumes {
		m := Mount{Type: v.Type, Source: v.Source, Target: v.Target, ReadOnly: v.ReadOnly, Consistency: v.Consistency}
		if vol, ok := p.Volumes[v.Source]; ok && v.Type == types.VolumeTypeVolume {
			m.Source = VolumeName(p.Name, v.Source, vol)
		}
		if b := v.Bind; b != nil {
			m.Bind = &BindOptions{Propagation: b.Propagation, SELinux: b.SELinux, CreateHostPath: bool(b.CreateHostPath), Recursive: b.Recursive}
		}
		if o := v.Volume; o != nil {
			m.Volume = &VolumeOptions{NoCopy: o.NoCopy, Subpath: o.Subpath, Labels: sortedKV(o.Labels)}
		}
		if t := v.Tmpfs; t != nil {
			m.Tmpfs = &TmpfsOptions{Size: int64(t.Size), Mode: t.Mode}
		}
		if i := v.Image; i != nil {
			m.Image = &ImageOptions{Subpath: i.SubPath}
		}
		spec.Mounts = append(spec.Mounts, m)
	}
	for _, r := range s.Secrets {
		if file := p.Secrets[r.Source].File; file != "" {
			spec.Mounts = append(spec.Mounts, Mount{Type: types.VolumeTypeBind, Source: file, Target: cmp.Or(r.Target, "/run/secrets/"+r.Source), ReadOnly: true})
		}
	}
	for _, r := range s.Configs {
		if file := p.Configs[r.Source].File; file != "" {
			spec.Mounts = append(spec.Mounts, Mount{Type: types.VolumeTypeBind, Source: file, Target: cmp.Or(r.Target, "/"+r.Source), ReadOnly: true})
		}
	}
	for _, v := range s.VolumesFrom {
		if c, ok := strings.CutPrefix(v, types.ContainerPrefix); ok {
			spec.VolumesFrom = append(spec.VolumesFrom, c)
			continue
		}
		svc, mode, ok := strings.Cut(v, ":")
		name := serviceContainerName(p, svc, 1)
		if ok {
			name += ":" + mode
		}
		spec.VolumesFrom = append(spec.VolumesFrom, name)
	}

	for _, d := range s.Devices {
		spec.Devices = append(spec.Devices, Device{Source: d.Source, Target: d.Target, Permissions: d.Permissions})
	}
	gpu := func(d types.DeviceRequest) {
		spec.Gpus = append(spec.Gpus, DeviceRequest{Driver: d.Driver, Count: int64(d.Count), IDs: d.IDs, Capabilities: d.Capabilities, Options: sortedKV(d.Options)})
	}
	for _, d := range s.Gpus {
		gpu(d)
	}
	if d := s.Deploy; d != nil {
		if rp := d.RestartPolicy; rp != nil && spec.Restart == "" {
			switch rp.Condition {
			case "any":
				spec.Restart = types.RestartPolicyAlways
			case "on-failure":
				spec.Restart = types.RestartPolicyOnFailure
				if rp.MaxAttempts != nil {
					spec.Restart += ":" + strconv.FormatUint(*rp.MaxAttempts, 10)
				}
			case "none":
				spec.Restart = types.RestartPolicyNo
			}
		}
		if l := d.Resources.Limits; l != nil {
			if l.NanoCPUs != 0 {
				spec.CPUs = widen(float32(l.NanoCPUs))
			}
			if l.MemoryBytes != 0 {
				spec.Memory = int64(l.MemoryBytes)
			}
			if l.Pids != 0 {
				spec.PidsLimit = l.Pids
			}
		}
		if r := d.Resources.Reservations; r != nil {
			if r.MemoryBytes != 0 {
				spec.MemoryReservation = int64(r.MemoryBytes)
			}
			for _, dev := range r.Devices {
				if slices.Contains(dev.Capabilities, "gpu") {
					gpu(dev)
				}
			}
		}
	}

	if b := s.BlkioConfig; b != nil {
		spec.Blkio = &Blkio{
			Weight:          b.Weight,
			DeviceReadBps:   throttleDevices(b.DeviceReadBps),
			DeviceWriteBps:  throttleDevices(b.DeviceWriteBps),
			DeviceReadIOps:  throttleDevices(b.DeviceReadIOps),
			DeviceWriteIOps: throttleDevices(b.DeviceWriteIOps),
		}
		for _, w := range b.WeightDevice {
			spec.Blkio.WeightDevice = append(spec.Blkio.WeightDevice, WeightDevice{Path: w.Path, Weight: w.Weight})
		}
	}
	if h := s.HealthCheck; h != nil {
		spec.Healthcheck = &Healthcheck{
			Test:          h.Test,
			Interval:      time.Duration(derefOrZero(h.Interval)),
			Timeout:       time.Duration(derefOrZero(h.Timeout)),
			StartPeriod:   time.Duration(derefOrZero(h.StartPeriod)),
			StartInterval: time.Duration(derefOrZero(h.StartInterval)),
			Retries:       int(derefOrZero(h.Retries)),
			Disable:       h.Disable,
		}
	}
	switch {
	case s.Logging != nil:
		spec.Logging = &Logging{Driver: s.Logging.Driver, Options: sortedKV(s.Logging.Options)}
	case s.LogDriver != "" || len(s.LogOpt) > 0:
		spec.Logging = &Logging{Driver: s.LogDriver, Options: sortedKV(s.LogOpt)}
	}
	return spec
}

func toBuildSpec(p *types.Project, key string, b *types.BuildConfig, tag string) BuildSpec {
	dockerfile := b.Dockerfile
	// The loader absolutizes local contexts and leaves remote ones (URLs, git@, github.com/...) untouched.
	if dockerfile != "" && !filepath.IsAbs(dockerfile) && filepath.IsAbs(b.Context) {
		dockerfile = filepath.Join(b.Context, dockerfile)
	}
	spec := BuildSpec{
		Service:            key,
		Tag:                tag,
		Context:            b.Context,
		Dockerfile:         dockerfile,
		Args:               sortedKV(b.Args.ToMapping()),
		Target:             b.Target,
		Labels:             sortedKV(b.Labels),
		NoCache:            b.NoCache,
		Pull:               b.Pull,
		Platforms:          b.Platforms,
		CacheFrom:          b.CacheFrom,
		CacheTo:            b.CacheTo,
		ExtraHosts:         sortedHosts(b.ExtraHosts),
		Network:            b.Network,
		ShmSize:            int64(b.ShmSize),
		AdditionalContexts: sortedKV(b.AdditionalContexts),
		Tags:               b.Tags,
		Ulimits:            ulimitList(b.Ulimits),
		Isolation:          b.Isolation,
		Privileged:         b.Privileged,
	}
	for _, k := range b.SSH {
		ssh := k.ID
		if k.Path != "" {
			ssh += "=" + k.Path
		}
		spec.SSH = append(spec.SSH, ssh)
	}
	slices.Sort(spec.SSH)
	for _, r := range b.Secrets {
		spec.Secrets = append(spec.Secrets, BuildSecret{ID: cmp.Or(r.Target, r.Source), Source: p.Secrets[r.Source].File})
	}
	return spec
}

func toNetworkSpec(p *types.Project, key string) NetworkSpec {
	n := p.Networks[key]
	spec := NetworkSpec{
		Name:        NetworkName(p.Name, key, n),
		Driver:      n.Driver,
		DriverOpts:  sortedKV(n.DriverOpts),
		IPAMDriver:  n.Ipam.Driver,
		IPAMOptions: sortedKV(n.Ipam.Options),
		Internal:    n.Internal,
		Attachable:  n.Attachable,
		EnableIPv4:  n.EnableIPv4,
		EnableIPv6:  n.EnableIPv6,
		Labels:      mergeLabels(n.Labels, map[string]string{LabelProject: p.Name, LabelNetwork: key}),
	}
	for _, pool := range n.Ipam.Config {
		spec.IPAM = append(spec.IPAM, IPAMPool{Subnet: pool.Subnet, Gateway: pool.Gateway, IPRange: pool.IPRange, AuxAddresses: sortedKV(pool.AuxiliaryAddresses)})
	}
	return spec
}

func toVolumeSpec(p *types.Project, key string) VolumeSpec {
	v := p.Volumes[key]
	return VolumeSpec{
		Name:       VolumeName(p.Name, key, v),
		Driver:     v.Driver,
		DriverOpts: sortedKV(v.DriverOpts),
		Labels:     mergeLabels(v.Labels, map[string]string{LabelProject: p.Name, LabelVolume: key}),
	}
}

func ignoredServiceAttrs(key string, s types.ServiceConfig) []Warning {
	var ws []Warning
	ignore := func(attr, reason string) {
		ws = append(ws, Warning{Scope: "services." + key, Attribute: attr, Message: "ignored (" + reason + ")"})
	}
	fileRef := func(kind string, r types.FileReferenceConfig) {
		if r.UID != "" {
			ignore(kind+"."+r.Source+".uid", "bind mounts keep host ownership")
		}
		if r.GID != "" {
			ignore(kind+"."+r.Source+".gid", "bind mounts keep host ownership")
		}
		if r.Mode != nil {
			ignore(kind+"."+r.Source+".mode", "bind mounts keep host permissions")
		}
	}
	for _, r := range s.Secrets {
		fileRef("secrets", types.FileReferenceConfig(r))
	}
	for _, r := range s.Configs {
		fileRef("configs", types.FileReferenceConfig(r))
	}
	if s.Develop != nil {
		ignore("develop", "not supported")
	}
	if s.Provider != nil {
		ignore("provider", "not supported")
	}
	if len(s.Models) > 0 {
		ignore("models", "not supported")
	}
	if s.UseAPISocket {
		ignore("use_api_socket", "not supported")
	}
	if s.Attach != nil && !*s.Attach {
		ignore("attach", "not supported")
	}
	if s.CredentialSpec != nil {
		ignore("credential_spec", "not supported")
	}
	if len(s.PreStart) > 0 {
		ignore("pre_start", "not supported")
	}
	if len(s.PostStart) > 0 {
		ignore("post_start", "not supported")
	}
	if len(s.PreStop) > 0 {
		ignore("pre_stop", "not supported")
	}
	if d := s.Deploy; d != nil {
		if d.Mode == "global" {
			ignore("deploy.mode", "swarm only")
		}
		if pl := d.Placement; len(pl.Constraints) > 0 || len(pl.Preferences) > 0 || pl.MaxReplicas != 0 {
			ignore("deploy.placement", "swarm only")
		}
		if d.UpdateConfig != nil {
			ignore("deploy.update_config", "swarm only")
		}
		if d.RollbackConfig != nil {
			ignore("deploy.rollback_config", "swarm only")
		}
		if d.EndpointMode != "" {
			ignore("deploy.endpoint_mode", "swarm only")
		}
		if len(d.Labels) > 0 {
			ignore("deploy.labels", "swarm only")
		}
		if r := d.Resources.Reservations; r != nil {
			if r.NanoCPUs != 0 {
				ignore("deploy.resources.reservations.cpus", "no engine equivalent")
			}
			if len(r.GenericResources) > 0 {
				ignore("deploy.resources.reservations.generic_resources", "swarm only")
			}
		}
	}
	if b := s.Build; b != nil {
		if b.DockerfileInline != "" {
			ignore("build.dockerfile_inline", "not supported")
		}
		if b.Provenance != "" {
			ignore("build.provenance", "not supported")
		}
		if b.SBOM != "" {
			ignore("build.sbom", "not supported")
		}
		if len(b.Entitlements) > 0 {
			ignore("build.entitlements", "not supported")
		}
	}
	return ws
}

func ignoredProjectAttrs(p *types.Project) []Warning {
	var ws []Warning
	source := func(kind, name string, f types.FileObjectConfig) {
		scope := kind + "s." + name
		switch {
		case bool(f.External):
			ws = append(ws, Warning{Scope: scope, Attribute: "external", Message: "ignored (only file sources are mounted)"})
		case f.Environment != "":
			ws = append(ws, Warning{Scope: scope, Attribute: "environment", Message: "ignored (only file sources are mounted)"})
		case f.Content != "": // the loader also fills content from environment, which is reported above
			ws = append(ws, Warning{Scope: scope, Attribute: "content", Message: "ignored (only file sources are mounted)"})
		}
	}
	for name, s := range p.Secrets {
		source("secret", name, types.FileObjectConfig(s))
	}
	for name, c := range p.Configs {
		source("config", name, types.FileObjectConfig(c))
	}
	for name := range p.Jobs {
		ws = append(ws, Warning{Scope: "jobs." + name, Attribute: "jobs", Message: "ignored (not supported)"})
	}
	for name := range p.Models {
		ws = append(ws, Warning{Scope: "models." + name, Attribute: "models", Message: "ignored (not supported)"})
	}
	return ws
}

func sortedKV[M ~map[string]string](m M) []KV {
	var kv []KV
	for _, k := range slices.Sorted(maps.Keys(m)) {
		kv = append(kv, KV{Key: k, Value: m[k]})
	}
	return kv
}

// mergeLabels lets compose labels override user labels with the same key.
func mergeLabels(user types.Labels, compose map[string]string) []KV {
	m := map[string]string{}
	maps.Copy(m, user)
	maps.Copy(m, compose)
	return sortedKV(m)
}

func sortedHosts(h types.HostsList) []string {
	if len(h) == 0 {
		return nil
	}
	l := h.AsList(":")
	slices.Sort(l)
	return l
}

func ulimitList(m map[string]*types.UlimitsConfig) []Ulimit {
	var l []Ulimit
	for _, name := range slices.Sorted(maps.Keys(m)) {
		u := Ulimit{Name: name, Soft: m[name].Soft, Hard: m[name].Hard}
		if m[name].Single != 0 {
			u.Soft, u.Hard = m[name].Single, m[name].Single
		}
		l = append(l, u)
	}
	return l
}

func throttleDevices(ds []types.ThrottleDevice) []ThrottleDevice {
	var l []ThrottleDevice
	for _, d := range ds {
		l = append(l, ThrottleDevice{Path: d.Path, Rate: int64(d.Rate)})
	}
	return l
}

func stopTimeout(d *types.Duration) *time.Duration {
	if d == nil {
		return nil
	}
	t := time.Duration(*d)
	return &t
}

func derefOrZero[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return v
}

// widen goes through the shortest decimal form so float32 0.1 becomes float64 0.1.
func widen(f float32) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	return v
}
