/*
Copyright 2024 EasyStack, Inc.
*/

package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	esv1 "github.com/easystack/accelerator-manager/api/v1"
)

const (
	NpuModeManagedByLabel     = "ecns.easystack.io/npu.deploy.managed-by"
	NpuConfigStateLabel       = "ecns.easystack.io/npu.config.state"
	NpuWorkloadConfigLabelKey = "ecns.easystack.io/npu.workload.config"
	NpuWorkerSelectorLabelKey = "workerselector"
)

const (
	NpuConfigStateLabelValueSuccess    = "success"
	NpuConfigStateLabelValueInProgress = "in-progress"
	NpuConfigStateLabelValueRestored   = "restored"
	NpuWorkerSelectorDLS               = "dls-worker-node"
)

var npuModeLabels = map[esv1.DeviceMode]map[string]string{
	esv1.Container: {
		NpuWorkloadConfigLabelKey: string(esv1.Container),
		NpuWorkerSelectorLabelKey: NpuWorkerSelectorDLS,
	},
	esv1.Virtualization: {
		NpuWorkloadConfigLabelKey: string(esv1.Virtualization),
		NpuWorkerSelectorLabelKey: NpuWorkerSelectorDLS,
	},
}

// NpuNodePolicyReconciler reconciles a NpuNodePolicy object
type NpuNodePolicyReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ecns.easystack.io,resources=npunodepolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ecns.easystack.io,resources=npunodepolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ecns.easystack.io,resources=npunodepolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NpuNodePolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *NpuNodePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var requeue bool
	_ = r.Log.WithValues("Reconciling NpuNodePolicy", req.NamespacedName)

	// Fetch the ClusterPolicy instance
	origin := &esv1.NpuNodePolicy{}
	err := r.Client.Get(ctx, req.NamespacedName, origin)
	if err != nil {
		r.Log.Error(err, "Failed to fetch NpuNodePolicy instance")
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
			r.Log.Info("Release nodes managed by NpuNodePolicy", "Name", origin.GetName())
			opts := []client.ListOption{
				client.MatchingLabels(origin.Spec.NodeSelector),
			}
			managedNodeList := &corev1.NodeList{}
			err = r.Client.List(ctx, managedNodeList, opts...)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("unable to list managed nodes, err %s", err.Error())
			}

			if requeue, err = r.ReleaseNodes(ctx, managedNodeList.Items); requeue {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return reconcile.Result{Requeue: requeue}, err
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

	instance := origin.DeepCopy()

	// 1. fetch all nodes
	opts := []client.ListOption{
		client.MatchingLabels(instance.Spec.NodeSelector),
	}
	claimNodeList := &corev1.NodeList{}
	err = r.Client.List(ctx, claimNodeList, opts...)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to list claimed nodes, err %s", err.Error())
	}

	opts = []client.ListOption{
		client.MatchingLabels{NpuModeManagedByLabel: instance.GetName()},
	}
	matchNodeList := &corev1.NodeList{}
	err = r.Client.List(ctx, matchNodeList, opts...)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to list matched nodes, err %s", err.Error())
	}

	// 2. filter nodes
	adopt, match, release := r.FilterNodes(claimNodeList.Items, matchNodeList.Items)

	InitNpuNodePolicyStatus(instance, claimNodeList.Items)

	// 4. setup nodes
	modeMgr := newNpuModeManager(instance, r.Log)

	err = r.AdoptNodes(ctx, adopt, modeMgr)
	if err != nil {
		r.Log.Error(err, "Failed to adopt nodes")
		requeue = true
	}

	err = r.UpdateNodes(ctx, match, modeMgr)
	if err != nil {
		r.Log.Error(err, "Failed to update nodes")
		requeue = true
	}

	requeue, err = r.ReleaseNodes(ctx, release)
	if err != nil {
		r.Log.Error(err, "Failed to release nodes")
	}

	if !reflect.DeepEqual(origin.Status, instance.Status) {
		err = r.Status().Update(ctx, instance)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status, %s", err.Error())
		}
	}

	if requeue {
		return ctrl.Result{Requeue: requeue}, fmt.Errorf("failed to setup nodes by %s", req.NamespacedName.String())
	}
	r.Log.Info("Sync NpuNodePolicy instance successfully")

	return ctrl.Result{}, nil
}

func (r *NpuNodePolicyReconciler) FilterNodes(claimNodes, matchNodes []corev1.Node) (adopt, match, release []corev1.Node) {
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

func (r *NpuNodePolicyReconciler) AdoptNodes(ctx context.Context, nodes []corev1.Node, manager *npuModeManager) error {
	var errList []string

	for _, node := range nodes {
		labels := node.GetLabels()
		if v, ok := labels[NpuModeManagedByLabel]; ok && v != manager.managedBy {
			r.Log.Info(fmt.Sprintf("WARNING: unable to adopt node %s by %s, node has been adopted by %s", node.GetName(), manager.managedBy, v))
			continue
		}
		modified := manager.updateNPUStateLabels(node.GetName(), labels)
		if !modified {
			r.Log.Info("Node had been adopted", "name", node.GetName())
			continue
		}
		// update node labels
		node.SetLabels(labels)
		err := r.Client.Update(ctx, &node)
		if err != nil {
			errList = append(errList, fmt.Sprintf("unable to add the NPU mode labels for node %s, err %s",
				node.GetName(), err.Error()))
			continue
		}
		r.Log.Info("Node is adopted", "name", node.GetName())
		UpdateNpuNodePolicyStatus(manager.instance, node.GetName(), esv1.Initialized, true)
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, " "))
	}
	return nil
}

func (r *NpuNodePolicyReconciler) UpdateNodes(ctx context.Context, nodes []corev1.Node, manager *npuModeManager) error {
	var errList []string
	var modified bool

	for _, node := range nodes {
		labels := node.GetLabels()
		patch := client.MergeFrom(node.DeepCopy())
		modified = manager.updateNPUStateLabels(node.GetName(), labels)
		if !modified {
			r.Log.Info("Node does not need to be updated", "name", node.GetName())
			continue
		}
		// update node labels
		node.SetLabels(labels)
		err := r.Client.Patch(ctx, &node, patch)
		if err != nil {
			errList = append(errList, fmt.Sprintf("unable to update the NPU mode labels for node %s, err %s",
				node.GetName(), err.Error()))
			continue
		}
		r.Log.Info("Node is updated", "name", node.GetName())
		UpdateNpuNodePolicyStatus(manager.instance, node.GetName(), esv1.Initialized, true)
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, " "))
	}
	return nil
}

func (r *NpuNodePolicyReconciler) ReleaseNodes(ctx context.Context, nodes []corev1.Node) (bool, error) {
	var errList []string
	var requeue bool

	for _, node := range nodes {
		labels := node.GetLabels()
		if labels[NpuConfigStateLabel] == NpuConfigStateLabelValueRestored {
			r.Log.Info("Node is released", "node", node.GetName())
			continue
		}
		requeue = true

		patch := client.MergeFrom(node.DeepCopy())

		modified := removeAllNPUModeLabels(labels)
		if !modified {
			r.Log.Info("Node are being released", "node", node.GetName())
			continue
		}
		// update node labels
		node.SetLabels(labels)

		err := r.Client.Patch(ctx, &node, patch)
		if err != nil {
			errList = append(errList, fmt.Sprintf("unable to reset the NPU mode labels for node %s, err %s",
				node.GetName(), err.Error()))
		}
	}
	if len(errList) > 0 {
		return requeue, fmt.Errorf(strings.Join(errList, " "))
	}
	return requeue, nil
}

func InitNpuNodePolicyStatus(instance *esv1.NpuNodePolicy, nodes []corev1.Node) {
	if instance.Status.Nodes == nil {
		instance.Status.Nodes = make(map[string]esv1.NpuNodeStatus)
	}
	for _, node := range nodes {
		UpdateNpuNodePolicyStatus(instance, node.GetName(), esv1.Initializing, false)
	}
}

func UpdateNpuNodePolicyStatus(instance *esv1.NpuNodePolicy, node string, status esv1.SyncStatus, force bool) {
	s, ok := instance.Status.Nodes[node]
	if !ok {
		s = esv1.NpuNodeStatus{}
	}
	if force || s.SyncStatus == "" {
		s.SyncStatus = status
	}

	instance.Status.Nodes[node] = s
}

// SetupWithManager sets up the controller with the Manager.
func (r *NpuNodePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&esv1.NpuNodePolicy{}).
		Complete(r)
}
