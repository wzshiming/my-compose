package plan

import "time"

// KV is a key-value pair; producers sort every []KV by Key.
type KV struct {
	Key   string
	Value string
}

type ContainerSpec struct {
	Service     string
	Number      int
	Name        string
	Image       string
	Platform    string
	PullPolicy  string // always, never or missing
	Labels      []KV
	Annotations []KV

	// A nil Command or Entrypoint keeps the image default; an empty one clears it.
	Command     []string
	Entrypoint  []string
	WorkingDir  string
	User        string
	Tty         bool
	StdinOpen   bool
	Init        *bool
	StopSignal  string
	StopTimeout *time.Duration
	Env         []KV
	Restart     string

	NetworkMode string
	Networks    []NetworkAttachment
	Hostname    string
	DomainName  string
	MacAddress  string
	DNS         []string
	DNSSearch   []string
	DNSOpts     []string
	ExtraHosts  []string
	Ports       []Port
	Expose      []string
	Links       []string

	Mounts       []Mount
	Tmpfs        []string
	ShmSize      int64
	VolumesFrom  []string
	VolumeDriver string
	StorageOpt   []KV
	ReadOnly     bool

	Privileged        bool
	CapAdd            []string
	CapDrop           []string
	SecurityOpt       []string
	GroupAdd          []string
	DeviceCgroupRules []string
	Devices           []Device
	Gpus              []DeviceRequest
	Sysctls           []KV
	Ulimits           []Ulimit
	Pid               string
	Ipc               string
	Uts               string
	Userns            string
	CgroupParent      string
	Cgroup            string
	Runtime           string
	Isolation         string

	CPUs              float64
	CPUShares         int64
	CPUSet            string
	CPUPeriod         int64
	CPUQuota          int64
	CPURTPeriod       int64
	CPURTRuntime      int64
	CPUCount          int64
	CPUPercent        float64
	Memory            int64
	MemoryReservation int64
	MemorySwap        int64
	MemorySwappiness  int64
	OomKillDisable    bool
	OomScoreAdj       int64
	PidsLimit         int64
	Blkio             *Blkio

	Healthcheck *Healthcheck
	Logging     *Logging
}

type NetworkAttachment struct {
	Key           string // compose network key; Name is the engine network name
	Name          string
	Aliases       []string
	IPv4          string
	IPv6          string
	MacAddress    string
	LinkLocalIPs  []string
	DriverOpts    []KV
	Priority      int
	GwPriority    int
	InterfaceName string
}

type Port struct {
	Name        string
	Mode        string
	HostIP      string
	Published   string
	Target      uint32
	Protocol    string
	AppProtocol string
}

type Mount struct {
	Type        string
	Source      string
	Target      string
	ReadOnly    bool
	Consistency string
	Bind        *BindOptions
	Volume      *VolumeOptions
	Tmpfs       *TmpfsOptions
	Image       *ImageOptions
}

type BindOptions struct {
	Propagation    string
	SELinux        string
	CreateHostPath bool
	Recursive      string
}

type VolumeOptions struct {
	NoCopy  bool
	Subpath string
	Labels  []KV
}

type TmpfsOptions struct {
	Size int64
	Mode uint32
}

type ImageOptions struct {
	Subpath string
}

type Device struct {
	Source      string
	Target      string
	Permissions string
}

type DeviceRequest struct {
	Driver       string
	Count        int64
	IDs          []string
	Capabilities []string
	Options      []KV
}

type Ulimit struct {
	Name string
	Soft int
	Hard int
}

type Blkio struct {
	Weight          uint16
	WeightDevice    []WeightDevice
	DeviceReadBps   []ThrottleDevice
	DeviceWriteBps  []ThrottleDevice
	DeviceReadIOps  []ThrottleDevice
	DeviceWriteIOps []ThrottleDevice
}

type WeightDevice struct {
	Path   string
	Weight uint16
}

type ThrottleDevice struct {
	Path string
	Rate int64
}

type Healthcheck struct {
	Test          []string
	Interval      time.Duration
	Timeout       time.Duration
	StartPeriod   time.Duration
	StartInterval time.Duration
	Retries       int
	Disable       bool
}

type Logging struct {
	Driver  string
	Options []KV
}

type NetworkSpec struct {
	Name        string
	Driver      string
	DriverOpts  []KV
	IPAMDriver  string
	IPAM        []IPAMPool
	IPAMOptions []KV
	Internal    bool
	Attachable  bool
	EnableIPv4  *bool
	EnableIPv6  *bool
	Labels      []KV
}

type IPAMPool struct {
	Subnet       string
	Gateway      string
	IPRange      string
	AuxAddresses []KV
}

type VolumeSpec struct {
	Name       string
	Driver     string
	DriverOpts []KV
	Labels     []KV
}

type BuildSpec struct {
	Service            string
	Tag                string
	Context            string
	Dockerfile         string // joined onto a local Context when relative
	Args               []KV
	Target             string
	Labels             []KV
	NoCache            bool
	Pull               bool
	Platforms          []string
	CacheFrom          []string
	CacheTo            []string
	ExtraHosts         []string
	Network            string
	ShmSize            int64
	SSH                []string
	Secrets            []BuildSecret
	AdditionalContexts []KV
	Tags               []string
	Ulimits            []Ulimit
	Isolation          string
	Privileged         bool
}

type BuildSecret struct {
	ID     string
	Source string
}
