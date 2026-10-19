package plan

import (
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

func TestNames(t *testing.T) {
	tests := []struct {
		name, got, want string
	}{
		{"ContainerName", ContainerName("demo", "web", 2), "demo-web-2"},
		{"ImageName", ImageName("demo", "web"), "demo-web"},
		{"NetworkName default", NetworkName("demo", "front", types.NetworkConfig{}), "demo_front"},
		{"NetworkName override", NetworkName("demo", "front", types.NetworkConfig{Name: "shared"}), "shared"},
		{"VolumeName default", VolumeName("demo", "data", types.VolumeConfig{}), "demo_data"},
		{"VolumeName override", VolumeName("demo", "data", types.VolumeConfig{Name: "shared"}), "shared"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
