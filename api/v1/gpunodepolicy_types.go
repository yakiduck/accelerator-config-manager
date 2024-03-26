/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"github.com/NVIDIA/mig-parted/pkg/types"
	dpv1 "github.com/easystack/accelerator-config-manager/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type DeviceMode string

const (
	Default     DeviceMode = "default"
	VCUDA       DeviceMode = "vcuda"
	TimeSlicing DeviceMode = "time-slicing"
	MIG         DeviceMode = "mig"
)

// GpuNodePolicySpec defines the desired state of GpuNodePolicy
type GpuNodePolicySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// NodeSelector selects the nodes to be configured
	NodeSelector map[string]string `json:"nodeSelector" yaml:"nodeSelector"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=99
	// Priority of the policy, higher priority policies can override lower ones.
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`
	// GpuSelector selects the GPUs to be configured
	//GpuSelector GpuSelector `json:"gpuSelector,omitempty" yaml:"gpuSelector,omitempty"`
	// +kubebuilder:validation:Enum=default;vcuda;time-slicing;mig
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="deviceMode is immutable"
	// The mode type for configured GPUs. Allowed value "default", "vcuda", "time-slicing", "mig".
	Mode DeviceMode `json:"deviceMode" yaml:"deviceMode"`

	VcudaConfig *VcudaConfigSpec `json:"vcuda,omitempty" yaml:"vcuda,omitempty"`

	TimeSlicingConfig *TimeSlicingConfigSpec `json:"timeSlicing,omitempty" yaml:"timeSlicing,omitempty"`

	MigConfig *MigConfigSpec `json:"mig,omitempty" yaml:"mig,omitempty"`
}

type GpuSelector struct {
	// The vendor hex code of GPU device. Allowed value "8086", "15b3".
	Vendor string `json:"vendor,omitempty" yaml:"vendor,omitempty"`
	// The device hex code of GPU device. Allowed value "0d58", "1572", "158b", "1013", "1015", "1017", "101b".
	DeviceID string `json:"deviceID,omitempty" yaml:"deviceID,omitempty"`
	// BDF of GPU.
	RootDevices []string `json:"rootDevices,omitempty" yaml:"rootDevices,omitempty"`
}

// MigConfigSpec defines the spec to declare the desired MIG configuration for a set of GPUs.
type MigConfigSpec struct {
	// +kubebuilder:validation:Enum=none;single;mixed
	Strategy    string               `json:"strategy"          yaml:"strategy"`
	ConfigSlice []MigConfigSpecSlice `json:"configs,omitempty" yaml:"configs,omitempty"`
}

type MigConfigSpecSlice struct {
	DeviceFilter string          `json:"device-filter,omitempty" yaml:"device-filter,flow,omitempty"`
	Devices      string          `json:"devices"                 yaml:"devices,flow"`
	MigDevices   types.MigConfig `json:"mig-devices"             yaml:"mig-devices"`
}

// TimeSlicingConfigSpec defines the set of replicas to be made for timeSlicing available resources.
type TimeSlicingConfigSpec struct {
	RenameByDefault            bool                      `json:"renameByDefault,omitempty"            yaml:"renameByDefault,omitempty"`
	FailRequestsGreaterThanOne bool                      `json:"failRequestsGreaterThanOne,omitempty" yaml:"failRequestsGreaterThanOne,omitempty"`
	Resources                  []dpv1.ReplicatedResource `json:"resources,omitempty"                  yaml:"resources,omitempty"`
}

type VcudaConfigSpec struct {
}

// GpuNodePolicyStatus defines the observed state of GpuNodePolicy
type GpuNodePolicyStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Nodes record all gpu sync status
	Nodes map[string]GpuNodeStatus `json:"nodes" yaml:"nodes"`
}

type GpuNodeStatus struct {
	TimeSlicingMode *TimeSlicingModeStatus `json:"timeSlicing,omitempty" yaml:"timeSlicing,omitempty"`
	MigMode         *MigModeStatus         `json:"mig,omitempty"         yaml:"mig,omitempty"`
	DefaultMode     *DefaultModeStatus     `json:"default,omitempty"       yaml:"default,omitempty"`
	VcudaMode       *VcudaModeStatus       `json:"vcuda,omitempty"       yaml:"vcuda,omitempty"`
}

type TimeSlicingModeStatus struct {
	Enabled      bool             `json:"enabled"      yaml:"enabled"`
	DevicePlugin ConfigSyncStatus `json:"devicePlugin" yaml:"devicePlugin"`
}

type MigModeStatus struct {
	Enabled      bool             `json:"enabled"      yaml:"enabled"`
	DevicePlugin ConfigSyncStatus `json:"devicePlugin" yaml:"devicePlugin"`
	MigParted    ConfigSyncStatus `json:"migParted"    yaml:"migParted"`
}

type DefaultModeStatus struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type VcudaModeStatus struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type ConfigSyncStatus struct {
	Sync bool `json:"sync" yaml:"sync"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.deviceMode`,description="The mode of GPU"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`,description="The creation date"

// GpuNodePolicy is the Schema for the gpunodepolicies API
type GpuNodePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GpuNodePolicySpec   `json:"spec,omitempty"`
	Status GpuNodePolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GpuNodePolicyList contains a list of GpuNodePolicy
type GpuNodePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GpuNodePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GpuNodePolicy{}, &GpuNodePolicyList{})
}
