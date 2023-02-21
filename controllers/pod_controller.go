/*
Copyright 2022.

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

package controllers

import (
	"context"
	"fmt"
	"github.com/mangohow/cloud-ide-k8s-controller/tools/statussync"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	statusInformer *statussync.StatusInformer
}

func NewPodReconciler(client client.Client, scheme *runtime.Scheme, statusSyncManager *statussync.StatusInformer) *PodReconciler {
	return &PodReconciler{Client: client, Scheme: scheme, statusInformer: statusSyncManager}
}

//+kubebuilder:rbac:groups=cloud-ide.my.domain,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cloud-ide.my.domain,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cloud-ide.my.domain,resources=pods/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Pod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// TODO(user): your logic here
	pod := &v1.Pod{}
	err := r.Client.Get(context.Background(), req.NamespacedName, pod)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "get pod")
			return ctrl.Result{Requeue: true}, err
		}

		return ctrl.Result{}, nil
	}
	fmt.Printf("name:%s, status:%s\n", pod.Name, pod.Status.Phase)
	// 通知对端Pod已经处于Running状态
	if pod.Status.Phase == v1.PodRunning {
		r.statusInformer.Sync(pod.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the StatusInformer.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&v1.Pod{}).
		Complete(r)
}
