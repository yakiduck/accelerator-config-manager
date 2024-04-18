/*
Copyright 2024 EasyStack, Inc.
*/

package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	esv1 "github.com/easystack/accelerator-manager/api/v1"
)

// NpuConfigReconciler reconciles a NpuConfig object
type NpuConfigReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	Node      string
	NpuPolicy string
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NpuConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *NpuConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("Reconciling NPU configurations", req.NamespacedName)

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

	node := &corev1.Node{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: r.Node}, node)
	if err != nil {
		r.Log.Error(err, "Failed to fetch node", "node", node.GetName())
		return reconcile.Result{}, err
	}
	nPatch := client.MergeFrom(node.DeepCopy())

	node.Labels[NpuConfigStateLabel] = NpuConfigStateLabelValueInProgress
	err = r.Client.Patch(ctx, node, nPatch)
	if err != nil {
		r.Log.Error(err, "Failed to patch node", "node", node.GetName())
		return reconcile.Result{}, err
	}

	if policyName, ok := node.Labels[NpuModeManagedByLabel]; !ok || !origin.ObjectMeta.DeletionTimestamp.IsZero() {
		err = r.Restore()
		if err != nil {
			r.Log.Error(err, "Failed to restore node", "node", node.GetName(), "policy", origin.GetName())
			return reconcile.Result{}, err
		}
		r.Log.Info("Node is restored", "node", r.Node)

		node.Labels[NpuConfigStateLabel] = NpuConfigStateLabelValueRestored
		err := r.Client.Patch(ctx, node, nPatch)
		if err != nil {
			r.Log.Error(err, "Failed to patch node", "node", node.GetName())
			return reconcile.Result{}, err
		}
		r.NpuPolicy = ""
		return reconcile.Result{}, nil
	} else if origin.GetName() != policyName {
		r.Log.Error(err, "NpuNodePolicy not match", "name", policyName)
		return reconcile.Result{}, err
	}

	err = r.ApplyVnpuConfig()
	if err != nil {
		r.Log.Error(err, "Failed to apply vnpu config")
		return reconcile.Result{}, err
	}

	r.Log.Info("Set up NPU successfully", "node", r.Node)

	node.Labels[NpuConfigStateLabel] = NpuConfigStateLabelValueSuccess
	err = r.Client.Patch(ctx, node, nPatch)
	if err != nil {
		r.Log.Error(err, "Failed to patch node", "node", node.GetName())
		return reconcile.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NpuConfigReconciler) ApplyVnpuConfig() error {
	// TODO(user): your logic here: setup vNPU

	return nil
}

func (r *NpuConfigReconciler) Restore() error {
	// TODO(user): restore logic here: delete all vNPU(if not used)

	return nil
}

func addWatchNPUNode(ctx context.Context, r *NpuConfigReconciler, c controller.Controller, mgr ctrl.Manager) error {
	// Define a mapping from the Node object in the event to one or more
	// ClusterPolicy objects to Reconcile
	mapFn := func(ctx context.Context, a client.Object) []reconcile.Request {
		// find all the NpuNodePolicy to trigger their reconciliation
		node := &corev1.Node{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: r.Node}, node)
		if err != nil {
			r.Log.Error(err, "Failed to fetch Node", "Name", r.Node)
			return []reconcile.Request{}
		}

		policyName, ok := node.GetLabels()[NpuModeManagedByLabel]
		if !ok {
			if r.NpuPolicy == "" {
				r.Log.Error(err, "Unable to get NpuNodePolicy", "Name", r.Node)
				return []reconcile.Request{}
			}
			// When deleting
			policyName = r.NpuPolicy
		}

		policyInstance := &esv1.NpuNodePolicy{}
		policyType := types.NamespacedName{Namespace: corev1.NamespaceAll, Name: policyName}
		err = r.Client.Get(ctx, policyType, policyInstance)
		if err != nil {
			r.Log.Error(err, "Unable to get NpuNodePolicy", "Name", policyName)
			return []reconcile.Request{}
		}

		r.Log.Info("Reconciliate NpuNodePolicy after node label update", "Name", policyName)

		return []reconcile.Request{{policyType}}
	}

	predicateNode := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			r.Log.V(4).Info("Create node in queue", "node", e.Object.GetName(), "result", e.Object.GetName() == r.Node)
			return e.Object.GetName() == r.Node
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			r.Log.V(4).Info("Update node in queue", "node", e.ObjectNew.GetName(), "result", e.ObjectNew.GetName() == r.Node)
			return e.ObjectNew.GetName() == r.Node
		},
	}

	predicateLabel := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			label, ok := e.Object.GetLabels()[NpuModeManagedByLabel]
			if ok {
				r.NpuPolicy = label
			}
			r.Log.V(4).Info("Create node label in queue", "label", e.Object.GetLabels()[NpuModeManagedByLabel], "result", ok)
			return ok
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			newLabel := e.ObjectNew.GetLabels()[NpuModeManagedByLabel]
			oldLabel := e.ObjectOld.GetLabels()[NpuModeManagedByLabel]
			if newLabel != "" && oldLabel != newLabel {
				r.NpuPolicy = newLabel
			}
			r.Log.V(4).Info("Update node label in queue", "label", e.ObjectNew.GetLabels()[NpuModeManagedByLabel], "result", oldLabel != newLabel)
			return oldLabel != newLabel
		},
	}

	err := c.Watch(
		source.Kind(mgr.GetCache(), &corev1.Node{}),
		handler.EnqueueRequestsFromMapFunc(mapFn),
		predicateNode,
		predicateLabel)

	return err
}

func addWatchNpuPolicy(ctx context.Context, r *NpuConfigReconciler, c controller.Controller, mgr ctrl.Manager) error {
	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			r.Log.V(4).Info("Create NpuNodePolicy in queue", "NpuNodePolicy", e.Object.GetName(), "result", e.Object.GetName() == r.NpuPolicy)
			return e.Object.GetName() == r.NpuPolicy
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			r.Log.V(4).Info("Update NpuNodePolicy in queue", "NpuNodePolicy", e.ObjectNew.GetName(), "result", e.ObjectNew.GetName() == r.NpuPolicy)
			return e.ObjectNew.GetName() == r.NpuPolicy
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			r.Log.V(4).Info("Delete NpuNodePolicy in queue", "NpuNodePolicy", e.Object.GetName(), "result", e.Object.GetName() == r.NpuPolicy)
			return e.Object.GetName() == r.NpuPolicy
		},
		GenericFunc: func(e event.GenericEvent) bool {
			r.Log.V(4).Info("Generic NpuNodePolicy in queue", "NpuNodePolicy", e.Object.GetName(), "result", e.Object.GetName() == r.NpuPolicy)
			return e.Object.GetName() == r.NpuPolicy
		},
	}

	err := c.Watch(source.Kind(mgr.GetCache(), &esv1.NpuNodePolicy{}),
		&handler.EnqueueRequestForObject{},
		p,
		predicate.GenerationChangedPredicate{})

	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *NpuConfigReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	c, err := controller.New("npu-config-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource NpuNodePolicy
	err = addWatchNpuPolicy(ctx, r, c, mgr)
	if err != nil {
		return err
	}

	// Watch for changes to Node labels and requeue the managed NpuNodePolicy
	err = addWatchNPUNode(ctx, r, c, mgr)
	if err != nil {
		return err
	}

	return nil
}
