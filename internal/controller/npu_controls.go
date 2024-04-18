package controller

import (
	esv1 "github.com/easystack/accelerator-manager/api/v1"
	"github.com/go-logr/logr"
)

type npuModeManager struct {
	instance  *esv1.NpuNodePolicy
	mode      esv1.DeviceMode
	managedBy string
	log       logr.Logger
}

func newNpuModeManager(instance *esv1.NpuNodePolicy, log logr.Logger) *npuModeManager {
	return &npuModeManager{
		instance:  instance,
		mode:      instance.Spec.Mode,
		managedBy: instance.GetName(),
		log:       log,
	}
}

func (m *npuModeManager) updateNPUStateLabels(node string, labels map[string]string) bool {
	removed := m.removeNPUModeLabels(node, labels)
	added := m.addNPUModeLabels(node, labels)
	return removed || added
}

func (m *npuModeManager) addNPUModeLabels(node string, labels map[string]string) bool {
	modified := false
	for key, value := range npuModeLabels[m.mode] {
		if v, ok := labels[key]; !ok || v != value {
			m.log.Info("Setting node label", "NodeName", node, "Label", key, "Value", value)
			labels[key] = value
			modified = true
		}
	}
	if v, ok := labels[NpuModeManagedByLabel]; !ok || v != m.managedBy {
		m.log.Info("Setting node label", "NodeName", node, "Label", NpuModeManagedByLabel, "Value", m.managedBy)
		labels[NpuModeManagedByLabel] = m.managedBy
		modified = true
	}
	return modified
}

func (m *npuModeManager) removeNPUModeLabels(node string, labels map[string]string) bool {
	modified := false
	for mode, labelsMap := range npuModeLabels {
		if mode == m.mode {
			continue
		}
		for key := range labelsMap {
			if _, ok := npuModeLabels[m.mode][key]; ok {
				// skip label if it is in the set of states for mode
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

func removeAllNPUModeLabels(labels map[string]string) bool {
	modified := false
	for _, labelsMap := range npuModeLabels {
		for key := range labelsMap {
			if _, ok := labels[key]; ok {
				delete(labels, key)
				modified = true
			}
		}
	}
	if _, ok := labels[NpuModeManagedByLabel]; ok {
		delete(labels, NpuModeManagedByLabel)
		modified = true
	}
	return modified
}
