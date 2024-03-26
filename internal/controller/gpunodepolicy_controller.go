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

package controller

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"time"

	nvdpv1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dpv1 "github.com/easystack/accelerator-config-manager/api/config/v1"
	esv1 "github.com/easystack/accelerator-config-manager/api/v1"
)

const (
	DevicePluginDefaultConfigLabel      = "nvidia.com/device-plugin.config"
	DevicePluginConfigLabelValueDefault = "default"
	MigConfigLabel                      = "nvidia.com/mig.config"
	MigConfigLabelValueAllDisabled      = "all-disabled"
	MigConfigStateLabel                 = "nvidia.com/mig.config.state"
	MigConfigStateLabelValueSuccess     = "success"
	//ModeDriverVersionLabel              = "nvidia.com/gpu.deploy.driver.version"
	ModeManagedByLabel = "ecns.easystack.io/gpu.deploy.managed-by"

	RELEASEFINALIZERNAME = "release.finalizers.ecns.easystack.io"
)

const (
	commonOperandsLabelKey     = "nvidia.com/gpu.deploy.operands"
	gpuWorkloadConfigLabelKey  = "nvidia.com/gpu.workload.config"
	gpuWorkloadConfigNone      = "none"
	gpuWorkloadConfigContainer = "container"
	gpuWorkloadConfigVcuda     = "vcuda"

	gpuProductLabelKey     = "nvidia.com/gpu.product"
	migManagerLabelKey     = "nvidia.com/gpu.deploy.mig-manager"
	migCapableLabelKey     = "nvidia.com/mig.capable"
	migCapableLabelValue   = "true"
	vgpuHostDriverLabelKey = "nvidia.com/vgpu.host-driver-version"

	// DevicePluginDefaultConfigMapName indicates name of ConfigMap containing default device plugin config
	DevicePluginDefaultConfigMapName = "nvidia-plugin-configs"
	// MigPartedDefaultConfigMapName indicates name of ConfigMap containing default mig-parted config
	MigPartedDefaultConfigMapName = "default-mig-parted-config"
)

var gpuModeLabels = map[string]map[string]string{
	gpuWorkloadConfigNone: {
		commonOperandsLabelKey: "false",
	},
	gpuWorkloadConfigContainer: {
		gpuWorkloadConfigLabelKey: gpuWorkloadConfigContainer,
	},
	gpuWorkloadConfigVcuda: {
		gpuWorkloadConfigLabelKey: gpuWorkloadConfigVcuda,
	},
}

// GpuNodePolicyReconciler reconciles a GpuNodePolicy object
type GpuNodePolicyReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	Namespace string
}

//+kubebuilder:rbac:groups=ecns.easystack.io,resources=gpunodepolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ecns.easystack.io,resources=gpunodepolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ecns.easystack.io,resources=gpunodepolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GpuNodePolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *GpuNodePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var requeue bool
	_ = r.Log.WithValues("Reconciling GpuNodePolicy", req.NamespacedName)

	// Fetch the ClusterPolicy instance
	origin := &esv1.GpuNodePolicy{}
	err := r.Client.Get(ctx, req.NamespacedName, origin)
	if err != nil {
		r.Log.Error(err, "Failed to fetch GpuNodePolicy instance")
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if origin.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !StringInArray(RELEASEFINALIZERNAME, origin.ObjectMeta.Finalizers) {
			origin.ObjectMeta.Finalizers = append(origin.ObjectMeta.Finalizers, RELEASEFINALIZERNAME)
			if err := r.Update(ctx, origin); err != nil {
				return reconcile.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if StringInArray(RELEASEFINALIZERNAME, origin.ObjectMeta.Finalizers) {
			// our finalizer is present, so lets handle any external dependency
			r.Log.Info("Release nodes managed by GpuNodePolicy", "Name", origin.GetName())
			opts := []client.ListOption{
				client.MatchingLabels{ModeManagedByLabel: origin.GetName()},
			}
			managedNodeList := &corev1.NodeList{}
			err = r.Client.List(ctx, managedNodeList, opts...)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("unable to list managed nodes, err %s", err.Error())
			}
			if origin.Spec.Mode == esv1.MIG {
				if ok := r.RevertMigNodes(ctx, managedNodeList.Items); !ok {
					r.Log.Info("Waiting for mig disabled on nodes", "Name", origin.GetName())
					return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
				}
			}
			if err := r.ReleaseNodes(ctx, managedNodeList.Items); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return reconcile.Result{}, err
			}

			r.Log.Info("Revert configmaps managed by GpuNodePolicy", "Name", origin.GetName())
			err = r.removeDevicePluginConfigMapData(ctx, origin)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("unable to revert device plugin configmap, err %s", err.Error())
			}
			err = r.removeMigConfigMapData(ctx, origin)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("unable to revert mig manager configmap, err %s", err.Error())
			}

			// remove our finalizer from the list and update it.
			var found bool
			origin.ObjectMeta.Finalizers, found = RemoveString(RELEASEFINALIZERNAME, origin.ObjectMeta.Finalizers)
			if found {
				if err := r.Update(ctx, origin); err != nil {
					return reconcile.Result{}, err
				}
			}
		}
		return reconcile.Result{}, err
	}

	manager := newGpuModeManager(origin.DeepCopy(), r.Log)

	// 1. fetch all nodes
	opts := []client.ListOption{
		client.MatchingLabels(manager.instance.Spec.NodeSelector),
	}
	claimNodeList := &corev1.NodeList{}
	err = r.Client.List(ctx, claimNodeList, opts...)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to list claimed nodes, err %s", err.Error())
	}

	opts = []client.ListOption{
		client.MatchingLabels{ModeManagedByLabel: manager.managedBy},
	}
	matchNodeList := &corev1.NodeList{}
	err = r.Client.List(ctx, matchNodeList, opts...)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to list matched nodes, err %s", err.Error())
	}

	// 2. filter nodes
	adopt, match, release := r.FilterNodes(claimNodeList.Items, matchNodeList.Items)

	// 3. set configmap
	switch manager.mode {
	case esv1.Default:
		// If default need config
	case esv1.VCUDA:
		// If gpu manager has config
	case esv1.TimeSlicing:
		err = r.setupTimeSlicingMode(ctx, manager)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to setup to time-slicing mode, err %s", err.Error())
		}
	case esv1.MIG:
		err = r.setupMigMode(ctx, manager)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to setup to mig mode, err %s", err.Error())
		}
	}

	InitGpuNodePolicyStatus(manager, claimNodeList.Items)

	err = r.AdoptNodes(ctx, adopt, manager)
	if err != nil {
		r.Log.Error(err, "Failed to adopt nodes")
		requeue = true
	}

	err = r.UpdateNodes(ctx, match, manager)
	if err != nil {
		r.Log.Error(err, "Failed to update nodes")
		requeue = true
	}

	err = r.ReleaseNodes(ctx, release)
	if err != nil {
		r.Log.Error(err, "Failed to release nodes")
		requeue = true
	}

	if !reflect.DeepEqual(origin.Status, manager.instance.Status) {
		err = r.Status().Update(ctx, manager.instance)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status, %s", err.Error())
		}
	}

	if requeue {
		return ctrl.Result{Requeue: requeue}, fmt.Errorf("failed to setup nodes by %s", req.NamespacedName.String())
	}
	r.Log.Info("Sync GpuNodePolicy instance successfully")

	return ctrl.Result{}, nil
}

func (r *GpuNodePolicyReconciler) setupTimeSlicingMode(ctx context.Context, manager *gpuModeManager) error {
	return r.addDevicePluginConfigMapData(ctx, manager)
}

func (r *GpuNodePolicyReconciler) setupMigMode(ctx context.Context, manager *gpuModeManager) error {
	if err := r.addDevicePluginConfigMapData(ctx, manager); err != nil {
		return fmt.Errorf("fail to setup to time-slicing config: %v", err)
	}
	if err := r.addMigConfigMapData(ctx, manager); err != nil {
		return fmt.Errorf("fail to setup to mig config: %v", err)
	}
	return nil
}

func (r *GpuNodePolicyReconciler) addDevicePluginConfigMapData(ctx context.Context, manager *gpuModeManager) error {
	origin := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: DevicePluginDefaultConfigMapName}, origin)
	if err != nil {
		return fmt.Errorf("unable to get device plugin ConfigMap, err %s", err.Error())
	}
	patch := client.MergeFrom(origin)
	dpConfigMap := origin.DeepCopy()

	manager.devicePluginConfigName = FindPrefixKey(origin.Data, fmt.Sprintf("%s-", manager.managedBy))

	oriRaw, ok := origin.Data[manager.devicePluginConfigName]
	if !ok && manager.instance.Spec.TimeSlicingConfig == nil && isMigNoneStrategy(manager.instance.Spec.MigConfig) {
		r.Log.Info("No time slicing config found, skip")
		return nil
	}
	oriConfig := &dpv1.Config{}
	_ = yaml.Unmarshal([]byte(oriRaw), oriConfig)

	dpConfig := &dpv1.Config{}
	convertSpecToDevicePluginConfig(manager.instance.Spec.TimeSlicingConfig, dpConfig)

	if manager.mode == esv1.MIG {
		strategy := setMigStrategy(manager.instance.Spec.MigConfig)
		dpConfig.Flags.MigStrategy = &strategy
		if strategy == nvdpv1.MigStrategySingle {
			dpConfig.Sharing = nil
		}
	}

	if reflect.DeepEqual(oriConfig, dpConfig) {
		r.Log.Info("No device plugin config need to be updated", "name", manager.devicePluginConfigName)
		return nil
	}

	raw, err := yaml.Marshal(dpConfig)
	if err != nil {
		return fmt.Errorf("marshal error: %v", err)
	}
	delete(dpConfigMap.Data, manager.devicePluginConfigName)
	manager.devicePluginConfigName = fmt.Sprintf("%s-%s", manager.managedBy, ComputeHash(string(raw)))
	dpConfigMap.Data[manager.devicePluginConfigName] = string(raw)

	err = r.Client.Patch(ctx, dpConfigMap, patch)
	if err != nil {
		return fmt.Errorf("fail to update device plugin ConfigMap, err %s", err.Error())
	}
	r.Log.Info("Device plugin config updated successfully", "name", manager.devicePluginConfigName)
	manager.devicePluginConfigUpdated = true

	return nil
}

func (r *GpuNodePolicyReconciler) removeDevicePluginConfigMapData(ctx context.Context, instance *esv1.GpuNodePolicy) error {
	dpConfigMap := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: DevicePluginDefaultConfigMapName}, dpConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Error(err, "DevicePlugin ConfigMap is not Found")
			return nil
		}
		return fmt.Errorf("unable to get device plugin ConfigMap, err %s", err.Error())
	}

	oldCfgName := FindPrefixKey(dpConfigMap.Data, fmt.Sprintf("%s-", instance.GetName()))

	if _, ok := dpConfigMap.Data[oldCfgName]; !ok {
		r.Log.Info("No device plugin config need to be reverted", "name", instance.GetName())
		return nil
	}

	patch := client.MergeFrom(dpConfigMap.DeepCopy())
	delete(dpConfigMap.Data, oldCfgName)
	err = r.Client.Patch(ctx, dpConfigMap, patch)
	if err != nil {
		return fmt.Errorf("fail to revert device plugin ConfigMap, err %s", err.Error())
	}
	r.Log.Info("Device plugin config is reverted", "name", instance.GetName())

	return nil
}

func (r *GpuNodePolicyReconciler) addMigConfigMapData(ctx context.Context, manager *gpuModeManager) error {
	origin := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: MigPartedDefaultConfigMapName}, origin)
	if err != nil {
		return fmt.Errorf("unable to get mig manager ConfigMap, err %s", err.Error())
	}
	patch := client.MergeFrom(origin)
	migConfigMap := origin.DeepCopy()

	oriConfig := &dpv1.Spec{}
	_ = yaml.Unmarshal([]byte(origin.Data["config.yaml"]), oriConfig)

	migConfig := &dpv1.Spec{}
	err = yaml.Unmarshal([]byte(migConfigMap.Data["config.yaml"]), migConfig)
	if err != nil {
		return fmt.Errorf("unmarshal error: %v", err)
	}

	manager.migConfigName = FindPrefixKey(oriConfig.MigConfigs, fmt.Sprintf("%s-", manager.managedBy))
	oldMigSpecSlice, ok := oriConfig.MigConfigs[manager.migConfigName]
	if !ok && isMigNoneStrategy(manager.instance.Spec.MigConfig) {
		r.Log.Info("No mig config found, skip")
		return nil
	}

	migSpecSlice := dpv1.MigConfigSpecSlice{}
	for _, config := range manager.instance.Spec.MigConfig.ConfigSlice {
		spec := dpv1.MigConfigSpec{}
		convertSpecToMigConfigSpec(&config, &spec)
		migSpecSlice = append(migSpecSlice, spec)
	}
	delete(migConfig.MigConfigs, manager.migConfigName)
	raw, _ := yaml.Marshal(migSpecSlice)
	manager.migConfigName = fmt.Sprintf("%s-%s", manager.managedBy, ComputeHash(string(raw)))
	migConfig.MigConfigs[manager.migConfigName] = migSpecSlice

	if reflect.DeepEqual(oldMigSpecSlice, migSpecSlice) {
		r.Log.Info("No mig manager config need to be updated", "name", manager.migConfigName)
		return nil
	}

	config, err := yaml.Marshal(migConfig)
	if err != nil {
		return fmt.Errorf("marshal error: %v", err)
	}
	migConfigMap.Data["config.yaml"] = string(config)

	err = r.Client.Patch(ctx, migConfigMap, patch)
	if err != nil {
		return fmt.Errorf("fail to update mig manager ConfigMap, err %s", err.Error())
	}
	r.Log.Info("MIG manager config updated successfully", "name", manager.migConfigName)
	manager.migConfigUpdated = true

	return nil
}

func (r *GpuNodePolicyReconciler) removeMigConfigMapData(ctx context.Context, instance *esv1.GpuNodePolicy) error {
	migConfigMap := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: MigPartedDefaultConfigMapName}, migConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Error(err, "MIG Manager ConfigMap is not Found")
			return nil
		}
		return fmt.Errorf("unable to get mig manager ConfigMap, err %s", err.Error())
	}
	patch := client.MergeFrom(migConfigMap.DeepCopy())

	migConfig := &dpv1.Spec{}
	err = yaml.Unmarshal([]byte(migConfigMap.Data["config.yaml"]), migConfig)
	if err != nil {
		return fmt.Errorf("unmarshal error: %v", err)
	}

	oldCfgName := FindPrefixKey(migConfig.MigConfigs, fmt.Sprintf("%s-", instance.GetName()))
	if _, ok := migConfig.MigConfigs[oldCfgName]; !ok {
		r.Log.Info("No mig config need to be reverted", "name", instance.GetName())
		return nil
	}
	delete(migConfig.MigConfigs, oldCfgName)

	config, err := yaml.Marshal(migConfig)
	if err != nil {
		return fmt.Errorf("marshal error: %v", err)
	}
	migConfigMap.Data["config.yaml"] = string(config)

	err = r.Client.Patch(ctx, migConfigMap, patch)
	if err != nil {
		return fmt.Errorf("fail to revert mig manager ConfigMap, err %s", err.Error())
	}
	r.Log.Info("MIG manager config is reverted", "name", instance.GetName())

	return nil
}

func convertSpecToDevicePluginConfig(in *esv1.TimeSlicingConfigSpec, out *dpv1.Config) {
	out.Version = nvdpv1.Version
	if in == nil {
		return
	}

	if out.Sharing == nil {
		out.Sharing = &dpv1.Sharing{}
	}
	out.Sharing.TimeSlicing.RenameByDefault = in.RenameByDefault
	out.Sharing.TimeSlicing.FailRequestsGreaterThanOne = in.FailRequestsGreaterThanOne
	out.Sharing.TimeSlicing.Resources = in.Resources
}

func convertSpecToMigConfigSpec(in *esv1.MigConfigSpecSlice, out *dpv1.MigConfigSpec) {
	if in == nil {
		return
	}
	out.Devices = in.Devices
	out.DeviceFilter = in.DeviceFilter
	out.MigEnabled = true
	out.MigDevices = in.MigDevices
}

func setMigStrategy(config *esv1.MigConfigSpec) string {
	if isMigNoneStrategy(config) {
		return nvdpv1.MigStrategyNone
	}
	return config.Strategy
}

func isMigNoneStrategy(config *esv1.MigConfigSpec) bool {
	return config == nil || len(config.ConfigSlice) == 0 || config.Strategy == ""
}

func (r *GpuNodePolicyReconciler) FilterNodes(claimNodes, matchNodes []corev1.Node) (adopt, match, release []corev1.Node) {
	claimNodeMap := make(map[string]corev1.Node, len(claimNodes))
	for _, claimNode := range claimNodes {
		claimNodeMap[claimNode.GetName()] = claimNode
	}
	for _, matchNode := range matchNodes {
		if _, ok := claimNodeMap[matchNode.GetName()]; !ok {
			release = append(release, matchNode)
			continue
		}
		r.Log.Info("Node is matched", "name", matchNode.GetName())
		match = append(match, matchNode)
		delete(claimNodeMap, matchNode.GetName())
	}
	for _, node := range claimNodeMap {
		adopt = append(adopt, node)
	}
	return
}

func (r *GpuNodePolicyReconciler) AdoptNodes(ctx context.Context, nodes []corev1.Node, manager *gpuModeManager) error {
	var errList []string

	for _, node := range nodes {
		labels := node.GetLabels()
		if v, ok := labels[ModeManagedByLabel]; ok && v != manager.managedBy {
			r.Log.Info(fmt.Sprintf("WARNING: unable to adopt node %s by %s, node has been adopted by %s", node.GetName(), manager.managedBy, v))
			continue
		}
		modified := manager.updateGPUStateLabels(node.GetName(), labels)
		if !modified {
			r.Log.Info("Node had been adopted", "name", node.GetName())
			continue
		}
		// update node labels
		node.SetLabels(labels)
		err := r.Client.Update(ctx, &node)
		if err != nil {
			errList = append(errList, fmt.Sprintf("unable to add the GPU mode labels for node %s, err %s",
				node.GetName(), err.Error()))
			continue
		}
		r.Log.Info("Node is adopted", "name", node.GetName())
		sync := false
		UpdateGpuNodePolicyStatus(manager.instance, node.GetName(), &sync, &sync)
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, " "))
	}
	return nil
}

func (r *GpuNodePolicyReconciler) UpdateNodes(ctx context.Context, nodes []corev1.Node, manager *gpuModeManager) error {
	var errList []string
	var modified bool

	for _, node := range nodes {
		labels := node.GetLabels()
		patch := client.MergeFrom(node.DeepCopy())
		modified = manager.updateGPUStateLabels(node.GetName(), labels)
		if !modified {
			r.Log.Info("Node does not need to be updated", "name", node.GetName())
			continue
		}
		// update node labels
		node.SetLabels(labels)
		err := r.Client.Patch(ctx, &node, patch)
		if err != nil {
			errList = append(errList, fmt.Sprintf("unable to update the GPU mode labels for node %s, err %s",
				node.GetName(), err.Error()))
			continue
		}
		r.Log.Info("Node is updated", "name", node.GetName())
		sync := false
		UpdateGpuNodePolicyStatus(manager.instance, node.GetName(), &sync, &sync)
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, " "))
	}
	return nil
}

func (r *GpuNodePolicyReconciler) ReleaseNodes(ctx context.Context, nodes []corev1.Node) error {
	var errList []string

	for _, node := range nodes {
		patch := client.MergeFrom(node.DeepCopy())

		labels := node.GetLabels()
		modified := removeAllGPUModeLabels(labels)
		if !modified {
			r.Log.Info("Node had been released", "node", node.GetName())
			continue
		}
		// update node labels
		node.SetLabels(labels)

		err := r.Client.Patch(ctx, &node, patch)
		if err != nil {
			errList = append(errList, fmt.Sprintf("unable to reset the GPU mode labels for node %s, err %s",
				node.GetName(), err.Error()))
			continue
		}
		r.Log.Info("Node is released", "node", node.GetName())
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, " "))
	}
	return nil
}

func (r *GpuNodePolicyReconciler) RevertMigNodes(ctx context.Context, nodes []corev1.Node) bool {
	done := true

	for _, node := range nodes {
		labels := node.GetLabels()
		if labels[MigConfigLabel] != MigConfigLabelValueAllDisabled {
			patch := client.MergeFrom(node.DeepCopy())

			labels[migManagerLabelKey] = "true"
			labels[MigConfigLabel] = MigConfigLabelValueAllDisabled
			delete(labels, MigConfigStateLabel)
			node.SetLabels(labels)

			err := r.Client.Patch(ctx, &node, patch)
			if err != nil {
				r.Log.Error(err, "Unable to revert mig mode", "node", node.GetName())
				done = false
				continue
			}
		}
		if labels[MigConfigStateLabel] != MigConfigStateLabelValueSuccess {
			r.Log.Info("MIG mode has not been reverted to completion", "currentState", labels[MigConfigStateLabel], "node", node.GetName())
			done = false
			continue
		}
		r.Log.Info("Node is reverted from mig mode", "node", node.GetName())
	}
	return done
}

// hasMIGCapableGPU returns true if this node has GPU capable of MIG partitioning.
/*func hasMIGCapableGPU(labels map[string]string) bool {
	if value, exists := labels[vgpuHostDriverLabelKey]; exists && value != "" {
		// vGPU node
		return false
	}

	if value, exists := labels[migCapableLabelKey]; exists {
		return value == migCapableLabelValue
	}

	// check product label if mig.capable label does not exist
	if value, exists := labels[gpuProductLabelKey]; exists {
		if strings.Contains(strings.ToLower(value), "h100") ||
			strings.Contains(strings.ToLower(value), "a100") ||
			strings.Contains(strings.ToLower(value), "a30") {
			return true
		}
	}

	return false
}*/

func InitGpuNodePolicyStatus(manager *gpuModeManager, nodes []corev1.Node) {
	if manager.instance.Status.Nodes == nil {
		manager.instance.Status.Nodes = make(map[string]esv1.GpuNodeStatus)
	}
	var needDPSync, needMigSync *bool
	if manager.devicePluginConfigUpdated {
		needDPSync = &manager.devicePluginConfigUpdated
	}
	if manager.migConfigUpdated {
		needMigSync = &manager.migConfigUpdated
	}
	for _, node := range nodes {
		UpdateGpuNodePolicyStatus(manager.instance, node.GetName(), needDPSync, needMigSync)
	}
}

func UpdateGpuNodePolicyStatus(instance *esv1.GpuNodePolicy, node string, needDPSync, needMigSync *bool) {
	status, ok := instance.Status.Nodes[node]
	if !ok {
		status = esv1.GpuNodeStatus{}
	}

	switch instance.Spec.Mode {
	case esv1.TimeSlicing:
		if status.TimeSlicingMode == nil {
			status.TimeSlicingMode = &esv1.TimeSlicingModeStatus{
				Enabled: true,
				DevicePlugin: esv1.ConfigSyncStatus{
					Sync: false,
				},
			}
		}
		if needDPSync != nil {
			status.TimeSlicingMode.DevicePlugin.Sync = *needDPSync
		}
		status.MigMode = nil
		status.DefaultMode = nil
		status.VcudaMode = nil
	case esv1.MIG:
		if status.MigMode == nil {
			status.MigMode = &esv1.MigModeStatus{
				Enabled: true,
				DevicePlugin: esv1.ConfigSyncStatus{
					Sync: false,
				},
				MigParted: esv1.ConfigSyncStatus{
					Sync: false,
				},
			}
		}
		if needDPSync != nil {
			status.MigMode.DevicePlugin.Sync = *needDPSync
		}
		if needMigSync != nil {
			status.MigMode.MigParted.Sync = *needMigSync
		}
		status.TimeSlicingMode = nil
		status.DefaultMode = nil
		status.VcudaMode = nil
	case esv1.Default:
		if status.DefaultMode == nil {
			status.DefaultMode = &esv1.DefaultModeStatus{
				Enabled: true,
			}
		}
		status.TimeSlicingMode = nil
		status.VcudaMode = nil
		status.MigMode = nil
	case esv1.VCUDA:
		if status.VcudaMode == nil {
			status.VcudaMode = &esv1.VcudaModeStatus{
				Enabled: true,
			}
		}
		status.TimeSlicingMode = nil
		status.DefaultMode = nil
		status.MigMode = nil
	}
	instance.Status.Nodes[node] = status
}

func StringInArray(val string, array []string) bool {
	for i := range array {
		if array[i] == val {
			return true
		}
	}
	return false
}

func RemoveString(s string, slice []string) (result []string, found bool) {
	if len(slice) != 0 {
		for _, item := range slice {
			if item == s {
				found = true
				continue
			}
			result = append(result, item)
		}
	}
	return
}

func FindPrefixKey[T any](m map[string]T, prefix string) string {
	for key := range m {
		if strings.HasPrefix(key, prefix) {
			return key
		}
	}
	return ""
}

func ComputeHash(str string) string {
	hash := md5.Sum([]byte(str))
	hashString := hex.EncodeToString(hash[:])
	return hashString[:6]
}

// SetupWithManager sets up the controller with the Manager.
func (r *GpuNodePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&esv1.GpuNodePolicy{}).
		Complete(r)
}
