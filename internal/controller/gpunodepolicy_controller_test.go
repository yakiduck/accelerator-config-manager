package controller

import (
	"fmt"
	"sigs.k8s.io/yaml"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	dpv1 "github.com/easystack/accelerator-manager/api/config/v1"
	esv1 "github.com/easystack/accelerator-manager/api/v1"
)

func TestConvert(t *testing.T) {
	in := &esv1.TimeSlicingConfigSpec{
		RenameByDefault:            true,
		FailRequestsGreaterThanOne: true,
		Resources: []dpv1.ReplicatedResource{
			{
				Name:     "nvidia.com/gpu",
				Replicas: 4,
			},
		},
	}

	out := &dpv1.Config{}

	convertSpecToDevicePluginConfig(in, out)
	strategy := setMigStrategy(nil)
	out.Flags.MigStrategy = &strategy

	raw, _ := yaml.Marshal(out)
	fmt.Printf("%s", string(raw))
}

func TestComputeHash(t *testing.T) {
	instance := &esv1.GpuNodePolicy{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
		Spec: esv1.GpuNodePolicySpec{
			Mode: esv1.TimeSlicing,
			TimeSlicingConfig: &esv1.TimeSlicingConfigSpec{
				RenameByDefault: true,
				Resources: []dpv1.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Replicas: 4,
					},
				},
			},
		},
	}

	manager := newGpuModeManager(instance, ctrl.Log)

	dpConfig := &dpv1.Config{}
	convertSpecToDevicePluginConfig(manager.instance.Spec.TimeSlicingConfig, dpConfig)

	raw, _ := yaml.Marshal(dpConfig)
	manager.devicePluginConfigName = fmt.Sprintf("%s-%s", manager.managedBy, ComputeHash(string(raw)))

	fmt.Println(manager.devicePluginConfigName)

}

func TestTimeSlicingNotUpdateLabels(t *testing.T) {
	instance := &esv1.GpuNodePolicy{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
		Spec: esv1.GpuNodePolicySpec{
			Mode: esv1.TimeSlicing,
			TimeSlicingConfig: &esv1.TimeSlicingConfigSpec{
				RenameByDefault: true,
				Resources: []dpv1.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Replicas: 4,
					},
				},
			},
		},
	}

	labels := map[string]string{
		gpuWorkloadConfigLabelKey:      gpuWorkloadConfigContainer,
		ModeManagedByLabel:             instance.GetName(),
		DevicePluginDefaultConfigLabel: instance.GetName(),
		MigConfigLabel:                 MigConfigLabelValueAllDisabled,
	}

	manager := newGpuModeManager(instance, ctrl.Log)

	modified := manager.updateGPUStateLabels("node-1", labels)

	fmt.Println(modified)

}

func TestMigNotUpdateLabels(t *testing.T) {
	instance := &esv1.GpuNodePolicy{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
		Spec: esv1.GpuNodePolicySpec{
			Mode: esv1.MIG,
		},
	}

	labels := map[string]string{
		gpuWorkloadConfigLabelKey:      gpuWorkloadConfigContainer,
		ModeManagedByLabel:             instance.GetName(),
		DevicePluginDefaultConfigLabel: instance.GetName(),
		MigConfigLabel:                 instance.GetName(),
	}

	manager := newGpuModeManager(instance, ctrl.Log)

	modified := manager.updateGPUStateLabels("node-1", labels)

	fmt.Println(modified)

}

func TestVcudaNotUpdateLabels(t *testing.T) {
	instance := &esv1.GpuNodePolicy{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
		Spec: esv1.GpuNodePolicySpec{
			Mode: esv1.VCUDA,
		},
	}

	labels := map[string]string{
		gpuWorkloadConfigLabelKey:      gpuWorkloadConfigVcuda,
		ModeManagedByLabel:             instance.GetName(),
		DevicePluginDefaultConfigLabel: DevicePluginConfigLabelValueDefault,
		MigConfigLabel:                 MigConfigLabelValueAllDisabled,
	}

	manager := newGpuModeManager(instance, ctrl.Log)

	modified := manager.updateGPUStateLabels("node-1", labels)

	fmt.Println(modified)

}

func TestUpdateStatus(t *testing.T) {
	instance := &esv1.GpuNodePolicy{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
		Spec: esv1.GpuNodePolicySpec{
			Mode: esv1.TimeSlicing,
		},
	}

	nodes := []corev1.Node{
		{
			ObjectMeta: v1.ObjectMeta{
				Name: "node-1",
			},
		},
	}

	manager := newGpuModeManager(instance, ctrl.Log)
	manager.devicePluginConfigUpdated = true

	InitGpuNodePolicyStatus(manager, nodes)

	fmt.Println(instance)
}
