/*
Copyright 2024 EasyStack, Inc.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	Container      DeviceMode = "container"
	Virtualization DeviceMode = "vm"
)

// NpuNodePolicySpec defines the desired state of NpuNodePolicy
type NpuNodePolicySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// NodeSelector selects the nodes to be configured
	NodeSelector map[string]string `json:"nodeSelector" yaml:"nodeSelector"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=99
	// Priority of the policy, higher priority policies can override lower ones.
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`
	// +kubebuilder:validation:Enum=container;vm
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="deviceMode is immutable"
	// The mode type for configured NPUs. Allowed value "container", "vm".
	Mode DeviceMode `json:"deviceMode" yaml:"deviceMode"`

	VnpuConfig *VnpuConfigSpec `json:"vnpu,omitempty" yaml:"vnpu,omitempty"`
}

type VnpuConfigSpec struct {
	ConfigSlice []VnpuConfigSpecSlice `json:"configs,omitempty" yaml:"configs,omitempty"`
}

type VnpuConfigSpecSlice struct {
	DeviceFilter []string   `json:"device-filter,omitempty" yaml:"device-filter,flow,omitempty"`
	Devices      []string   `json:"devices"                 yaml:"devices,flow"`
	VnpuDevices  VnpuConfig `json:"vnpu-devices"            yaml:"vnpu-devices"`
}

type VnpuConfig map[string]string

// NpuNodePolicyStatus defines the observed state of NpuNodePolicy
type NpuNodePolicyStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Nodes record all gpu sync status
	Nodes map[string]NpuNodeStatus `json:"nodes" yaml:"nodes"`
}

type SyncStatus string

const (
	Initializing SyncStatus = "Initializing"
	Initialized  SyncStatus = "Initialized"
	InProgress   SyncStatus = "InProgress"
	Succeeded    SyncStatus = "Succeeded"
)

type VnpuTemplates map[string]VTemplate
type VTemplate struct{}

type NpuNodeStatus struct {
	SyncStatus         SyncStatus     `json:"syncStatus"         yaml:"syncStatus"`
	AvailableTemplates VnpuTemplates  `json:"availableTemplates,omitempty" yaml:"availableTemplates,omitempty"`
	DeviceStatuses     []DeviceStatus `json:"deviceStatuses,omitempty"     yaml:"deviceStatuses,omitempty"`
}

type DeviceStatus struct {
	CardID       int          `json:"cardID" yaml:"cardID"`
	Health       string       `json:"health" yaml:"health"`
	VnpuStatuses []VnpuStatus `json:"vnpu"   yaml:"vnpu"`
}

type VnpuStatus struct {
	Id           int    `json:"id"           yaml:"id"`
	VgroupId     int    `json:"vgroupId"     yaml:"vgroupId"`
	ContainerId  int    `json:"containerId"  yaml:"containerId"`
	Status       int    `json:"status"       yaml:"status"`
	TemplateName string `json:"templateName" yaml:"templateName"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.deviceMode`,description="The mode of NPU"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`,description="The creation date"

// NpuNodePolicy is the Schema for the npunodepolicies API
type NpuNodePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NpuNodePolicySpec   `json:"spec,omitempty"`
	Status NpuNodePolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NpuNodePolicyList contains a list of NpuNodePolicy
type NpuNodePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NpuNodePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NpuNodePolicy{}, &NpuNodePolicyList{})
}
