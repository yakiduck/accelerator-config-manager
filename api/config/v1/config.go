package v1

import (
	nvdpv1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/NVIDIA/mig-parted/pkg/types"
)

// Copy from github.com/NVIDIA/k8s-device-plugin@v0.15.0:api/config/v1

// Config is a versioned struct used to hold configuration information.
type Config struct {
	Version string           `json:"version"             yaml:"version"`
	Flags   CommandLineFlags `json:"flags,omitempty"     yaml:"flags,omitempty"`
	Sharing *Sharing         `json:"sharing,omitempty"   yaml:"sharing,omitempty"`
}

// CommandLineFlags holds the list of command line flags used to configure the device plugin and GFD.
type CommandLineFlags struct {
	MigStrategy       *string                        `json:"migStrategy,omitempty"       yaml:"migStrategy,omitempty"`
	FailOnInitError   *bool                          `json:"failOnInitError,omitempty"   yaml:"failOnInitError,omitempty"`
	MpsRoot           *string                        `json:"mpsRoot,omitempty"           yaml:"mpsRoot,omitempty,omitempty"`
	NvidiaDriverRoot  *string                        `json:"nvidiaDriverRoot,omitempty"  yaml:"nvidiaDriverRoot,omitempty"`
	GDSEnabled        *bool                          `json:"gdsEnabled,omitempty"        yaml:"gdsEnabled,omitempty"`
	MOFEDEnabled      *bool                          `json:"mofedEnabled,omitempty"      yaml:"mofedEnabled,omitempty"`
	UseNodeFeatureAPI *bool                          `json:"useNodeFeatureAPI,omitempty" yaml:"useNodeFeatureAPI,omitempty"`
	Plugin            *nvdpv1.PluginCommandLineFlags `json:"plugin,omitempty"            yaml:"plugin,omitempty"`
	GFD               *nvdpv1.GFDCommandLineFlags    `json:"gfd,omitempty"               yaml:"gfd,omitempty"`
}

// Sharing encapsulates the set of sharing strategies that are supported.
type Sharing struct {
	// TimeSlicing defines the set of replicas to be made for timeSlicing available resources.
	TimeSlicing ReplicatedResources `json:"timeSlicing,omitempty" yaml:"timeSlicing,omitempty"`
	// MPS defines the set of replicas to be shared using MPS
	MPS *ReplicatedResources `json:"mps,omitempty"         yaml:"mps,omitempty"`
}

// ReplicatedResources defines generic options for replicating devices.
type ReplicatedResources struct {
	RenameByDefault            bool                 `json:"renameByDefault,omitempty"            yaml:"renameByDefault,omitempty"`
	FailRequestsGreaterThanOne bool                 `json:"failRequestsGreaterThanOne,omitempty" yaml:"failRequestsGreaterThanOne,omitempty"`
	Resources                  []ReplicatedResource `json:"resources,omitempty"                  yaml:"resources,omitempty"`
}

// ReplicatedResource represents a resource to be replicated.
type ReplicatedResource struct {
	Name     nvdpv1.ResourceName `json:"name"             yaml:"name"`
	Rename   nvdpv1.ResourceName `json:"rename,omitempty" yaml:"rename,omitempty"`
	Replicas int                 `json:"replicas"         yaml:"replicas"`
}

//Copy from github.com/NVIDIA/mig-parted@v0.6.0:api/spec/v1/spec.go

// Spec is a versioned struct used to hold information on 'MigConfigs'.
type Spec struct {
	Version    string                        `json:"version"               yaml:"version"`
	MigConfigs map[string]MigConfigSpecSlice `json:"mig-configs,omitempty" yaml:"mig-configs,omitempty"`
}

// MigConfigSpec defines the spec to declare the desired MIG configuration for a set of GPUs.
type MigConfigSpec struct {
	DeviceFilter interface{}     `json:"device-filter,omitempty" yaml:"device-filter,flow,omitempty"`
	Devices      interface{}     `json:"devices"                 yaml:"devices,flow"`
	MigEnabled   bool            `json:"mig-enabled"             yaml:"mig-enabled"`
	MigDevices   types.MigConfig `json:"mig-devices,omitempty"   yaml:"mig-devices,omitempty"`
}

// MigConfigSpecSlice represents a slice of 'MigConfigSpec'.
type MigConfigSpecSlice []MigConfigSpec
