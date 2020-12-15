/*
Copyright 2020 binbin.zou@kyligence.io.

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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cachev1alpha1 "github.com/zbbkeepgoing/memcached-operator/api/v1alpha1"
)

// MemcachedReconciler reconciles a Memcached object
type MemcachedReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cache.kyligence.io,resources=memcacheds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cache.kyligence.io,resources=memcacheds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cache.example.com,resources=memcacheds/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;create;watch;update;patch;delete;

func (r *MemcachedReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	reqLogger := r.Log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling Memcached Beginning")

	// Look up the memcached instance for this reconcile request
	memcached := &cachev1alpha1.Memcached{}
	err := r.Get(ctx, req.NamespacedName, memcached)
	// your logic here
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	reqLogger.Info("Find a Memcached: ", memcached.Name)
	lbls := labels.Set{
		"app":     memcached.Name,
		"version": "v0.1",
	}

	existingPods := &corev1.PodList{}
	err = r.Client.List(ctx, existingPods, &client.ListOptions{
		Namespace:     req.Namespace,
		LabelSelector: labels.SelectorFromSet(lbls),
	})
	if err != nil {
		reqLogger.Error(err, "failed to list existing pods in the memcached")
		return ctrl.Result{}, err
	}
	existingPodsNames := []string{}
	for _, pod := range existingPods.Items {
		// skip pod which is in deleting
		if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			existingPodsNames = append(existingPodsNames, pod.GetObjectMeta().GetName())
		}
	}

	reqLogger.Info("Checking Memcached", "expected replicas", memcached.Spec.Size, "Pod.Names", existingPodsNames)
	status := cachev1alpha1.MemcachedStatus{
		Replicas: int32(len(existingPodsNames)),
		PodNames: existingPodsNames,
	}
	if !reflect.DeepEqual(memcached.Status, status) {
		memcached.Status = status
		err = r.Client.Status().Update(ctx, memcached)
		if err != nil {
			reqLogger.Error(err, "Failed to update the memcached")
			return ctrl.Result{}, err
		}
	}
	// scale down pods
	if int32(len(existingPodsNames)) > memcached.Spec.Size {
		// delete a pod. Just one at a time
		reqLogger.Info("Deleting a pod in memcached", "expected replicas", memcached.Spec.Size, "Pod.Names", existingPodsNames)
		pod := existingPods.Items[0]
		err = r.Client.Delete(ctx, &pod)
		if err != nil {
			reqLogger.Error(err, "Failed to  delete a pod")
			return ctrl.Result{}, err
		}
	}

	// scale up pods
	if int32(len(existingPodsNames)) < memcached.Spec.Size {
		reqLogger.Info("Adding a pod in the memcached", "expected replicas", memcached.Spec.Size, "Pod.Names", existingPodsNames)
		pod := newPodForCR(memcached)
		if err = controllerutil.SetControllerReference(memcached, pod, r.Scheme); err != nil {
			reqLogger.Error(err, "unable to set owner reference on new pod")
			return ctrl.Result{}, err
		}
		err = r.Client.Create(ctx, pod)
		if err != nil {
			reqLogger.Error(err, "failed to create a pod")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *MemcachedReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1alpha1.Memcached{}).
		Owns(&appsv1.Deployment{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 2,
		}).
		Complete(r)
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(cr *cachev1alpha1.Memcached) *corev1.Pod {
	labels := map[string]string{
		"app":     cr.Name,
		"version": "v0.1",
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cr.Name + "-pod",
			Namespace:    cr.Namespace,
			Labels:       labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: cr.Spec.Image,
				},
			},
		},
	}
}
