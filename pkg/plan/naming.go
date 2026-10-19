package plan

import (
	"strconv"

	"github.com/compose-spec/compose-go/v2/types"
)

const (
	LabelProject         = "com.docker.compose.project"
	LabelService         = "com.docker.compose.service"
	LabelContainerNumber = "com.docker.compose.container-number"
	LabelOneoff          = "com.docker.compose.oneoff"
	LabelWorkingDir      = "com.docker.compose.project.working_dir"
	LabelConfigFiles     = "com.docker.compose.project.config_files"
	LabelNetwork         = "com.docker.compose.network"
	LabelVolume          = "com.docker.compose.volume"
)

func ContainerName(project, service string, n int) string {
	return project + "-" + service + "-" + strconv.Itoa(n)
}

func NetworkName(project, key string, cfg types.NetworkConfig) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	return project + "_" + key
}

func VolumeName(project, key string, cfg types.VolumeConfig) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	return project + "_" + key
}

func ImageName(project, service string) string {
	return project + "-" + service
}
