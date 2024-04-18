package controller

import (
	esv1 "github.com/easystack/accelerator-manager/api/v1"
	"github.com/go-logr/logr"
)

type gpuModeManager struct {
	instance                  *esv1.GpuNodePolicy
	mode                      esv1.DeviceMode
	workloadConfigName        string
	devicePluginConfigName    string
	devicePluginConfigUpdated bool
	migConfigName             string
	migConfigUpdated          bool
	managedBy                 string
	log                       logr.Logger
}

func newGpuModeManager(instance *esv1.GpuNodePolicy, log logr.Logger) *gpuModeManager {
	manager := &gpuModeManager{
		instance:           instance,
		mode:               instance.Spec.Mode,
		workloadConfigName: setWorkloadConfig(instance.Spec.Mode),
		managedBy:          instance.GetName(),
		log:                log,
	}
	if manager.mode == esv1.Default {
		manager.devicePluginConfigName = DevicePluginConfigLabelValueDefault
	}
	if manager.mode != esv1.MIG {
		manager.migConfigName = MigConfigLabelValueAllDisabled
	}

	return manager
}

func (m *gpuModeManager) updateGPUStateLabels(node string, labels map[string]string) bool {
	removed := m.removeGPUModeLabels(node, labels)
	added := m.addGPUModeLabels(node, labels)
	return removed || added
}

func (m *gpuModeManager) addGPUModeLabels(node string, labels map[string]string) bool {
	modified := false
	for key, value := range gpuModeLabels[m.workloadConfigName] {
		if v, ok := labels[key]; !ok || v != value {
			m.log.Info("Setting node label", "NodeName", node, "Label", key, "Value", value)
			labels[key] = value
			modified = true
		}
	}
	if v, ok := labels[ModeManagedByLabel]; !ok || v != m.managedBy {
		m.log.Info("Setting node label", "NodeName", node, "Label", ModeManagedByLabel, "Value", m.managedBy)
		labels[ModeManagedByLabel] = m.managedBy
		modified = true
	}
	if v := labels[DevicePluginDefaultConfigLabel]; m.devicePluginConfigName != "" && v != m.devicePluginConfigName {
		m.log.Info("Setting node label", "NodeName", node, "Label", DevicePluginDefaultConfigLabel, "Value", m.devicePluginConfigName)
		labels[DevicePluginDefaultConfigLabel] = m.devicePluginConfigName
		modified = true
	}
	if v := labels[MigConfigLabel]; m.migConfigName != "" && v != m.migConfigName {
		m.log.Info("Setting node label", "NodeName", node, "Label", MigConfigLabel, "Value", m.migConfigName)
		labels[MigConfigLabel] = m.migConfigName
		modified = true
	}
	return modified
}

func (m *gpuModeManager) removeGPUModeLabels(node string, labels map[string]string) bool {
	modified := false
	for workloadConfig, labelsMap := range gpuModeLabels {
		if workloadConfig == m.workloadConfigName {
			continue
		}
		for key := range labelsMap {
			if _, ok := gpuModeLabels[m.workloadConfigName][key]; ok {
				// skip label if it is in the set of states for modeConfig
				continue
			}
			if _, ok := labels[key]; ok {
				m.log.Info("Deleting node label", "NodeName", node, "Label", key)
				delete(labels, key)
				modified = true
			}
		}
	}
	return modified
}

func (m *gpuModeManager) isDevicePluginConfigUpdated(node string) bool {
	status, ok := m.instance.Status.Nodes[node]
	if ok {
		switch m.instance.Spec.Mode {
		case esv1.TimeSlicing:
			return status.TimeSlicingMode.DevicePlugin.Sync
		case esv1.MIG:
			return status.MigMode.DevicePlugin.Sync
		}
	}
	return false
}

func (m *gpuModeManager) isMigConfigUpdated(node string) bool {
	status, ok := m.instance.Status.Nodes[node]
	if ok {
		switch m.instance.Spec.Mode {
		case esv1.MIG:
			return status.MigMode.MigParted.Sync
		}
	}
	return false
}

func removeAllGPUModeLabels(labels map[string]string) bool {
	modified := false
	for k, labelsMap := range gpuModeLabels {
		if k == gpuWorkloadConfigNone {
			continue
		}
		for key := range labelsMap {
			if _, ok := labels[key]; ok {
				delete(labels, key)
				modified = true
			}
		}
	}
	if _, ok := labels[ModeManagedByLabel]; ok {
		delete(labels, ModeManagedByLabel)
		modified = true
	}
	if _, ok := labels[DevicePluginDefaultConfigLabel]; ok {
		delete(labels, DevicePluginDefaultConfigLabel)
		modified = true
	}
	if _, ok := labels[MigConfigLabel]; ok {
		delete(labels, MigConfigLabel)
		modified = true
	}
	if v, ok := labels[commonOperandsLabelKey]; !ok || v != "false" {
		labels[commonOperandsLabelKey] = "false"
		modified = true
	}
	return modified
}

func setWorkloadConfig(mode esv1.DeviceMode) string {
	switch mode {
	case esv1.VCUDA:
		return gpuWorkloadConfigVcuda
	case esv1.Default, esv1.TimeSlicing, esv1.MIG:
		return gpuWorkloadConfigContainer
	case esv1.PassThrough:
		return gpuWorkloadConfigVMPassthrough
	case "":
		return gpuWorkloadConfigNone
	}
	return gpuWorkloadConfigNone
}
