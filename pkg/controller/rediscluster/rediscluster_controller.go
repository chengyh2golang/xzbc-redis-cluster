package rediscluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"xzbc-redis-cluster/pkg/resources/configmap"
	"xzbc-redis-cluster/pkg/resources/job"
	"xzbc-redis-cluster/pkg/resources/service"
	"xzbc-redis-cluster/pkg/resources/statefulset"

	"k8s.io/client-go/util/retry"
	crdv1alpha1 "xzbc-redis-cluster/pkg/apis/crd/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_rediscluster")
//var redisClusterSize = sync.Map{}

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new RedisCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileRedisCluster{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("rediscluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource RedisCluster
	err = c.Watch(&source.Kind{Type: &crdv1alpha1.RedisCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner RedisCluster
	/*
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &crdv1alpha1.RedisCluster{},
	})
	if err != nil {
		return err
	}
	 */

	err = c.Watch(&source.Kind{Type: &batchv1.Job{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &crdv1alpha1.RedisCluster{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileRedisCluster implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileRedisCluster{}

// ReconcileRedisCluster reconciles a RedisCluster object
type ReconcileRedisCluster struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a RedisCluster object and makes changes based on the state read
// and what is in the RedisCluster.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileRedisCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling RedisCluster")

	// Fetch the RedisCluster instance
	instance := &crdv1alpha1.RedisCluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	//判断instance的DeletionTimestamp是否有值，
	// 如果有值，说明要被删除了，就直接返回，走k8s的垃圾回收机制
	if instance.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	//如果查到了，并且不是被删除，就判断它所关联的资源是否存在
	// Check if this Pod already exists
	found := &appsv1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {

		//创建redis配置文件需要用到的configMap
		cm := configmap.New(instance)
		err = r.client.Create(context.TODO(), cm)
		if err != nil {
			return reconcile.Result{}, err
		}

		//创建headless service
		headlessSvc := service.New(instance)
		err = r.client.Create(context.TODO(), headlessSvc)
		if err != nil {
			return reconcile.Result{}, err
		}

		//创建集群对外服务的svc
		clusterSvc := service.NewClusterSvc(instance)
		err = r.client.Create(context.TODO(), clusterSvc)
		if err != nil {
			return reconcile.Result{}, err
		}

		//创建准备做redis-trib的job
		redisTribJob := job.New(instance)
		if err := controllerutil.SetControllerReference(instance, redisTribJob, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
		err = r.client.Create(context.TODO(), redisTribJob)
		if err != nil {
			return reconcile.Result{}, err
		}

		/*
		pod := newPodForCR(instance)
		// Set App instance as the owner and controller
		//这是operator框架自带的设置ownerreference的方法
		if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			return reconcile.Result{}, err
		}
		 */

		sts := statefulset.New(instance)
		err = r.client.Create(context.TODO(), sts)
		if err != nil {
			//如果创建sts报错，把之前创建的资源删除后再返回错误
			go r.client.Delete(context.TODO(), headlessSvc)
			go r.client.Delete(context.TODO(), cm)
			go r.client.Delete(context.TODO(), clusterSvc)
			go r.client.Delete(context.TODO(), redisTribJob)
			return reconcile.Result{}, err
		}

		//创建完成之后还得去做一次更新
		//把对应的annotation给更新上，因为后面需要用annotation去做判断是否需要去做更新操作
		instance.Annotations = map[string]string{
			"crd.xzbc.com.cn/spec":toString(instance),
		}
		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.client.Update(context.TODO(), instance)
		})
		if retryErr != nil {
			fmt.Println(retryErr.Error())
		}

		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	//instance.Annotations["crd.xzbc.com.cn/spec"]这是老的信息
	//instance.spec是最新的信息，使用DeepEqual方法比较是否相等
	if ! reflect.DeepEqual(instance.Spec,toSpec(instance.Annotations["crd.xzbc.com.cn/spec"])) {
		//如果不相等，就需要去更新，更新就是重建sts和svc
		//但是更新操作通常是不会去更新svc的，只需要更新sts
		//TODO 更新操作（增加副本，删除副本）还需要有reids-trib的实现
		//现在的需求集中在集群的创建，还不涉及到更新集群，所以留给todo去做

		oldClusterSize := fmt.Sprintf("%v",*found.Spec.Replicas)
		newClusterSize := fmt.Sprintf("%v",*instance.Spec.Replicas)
		fmt.Printf("oldSize: %v, newSize: %v",oldClusterSize, newClusterSize)

		sts := statefulset.New(instance)
		found.Spec = sts.Spec

		//创建扩展rediscluster需要的configmap
		fmt.Println("准备创建scale需要的configmap")
		newScaleConfigMap := configmap.NewScaleConfigMap(instance, oldClusterSize, newClusterSize)

		fmt.Println("准备创建scale需要的configmap")
		err := r.client.Update(context.TODO(), newScaleConfigMap)
		fmt.Printf("创建configmap的报错信息：%v",err)
		if err != nil {
			return reconcile.Result{}, err
		}

		//创建scale job
		fmt.Println("准备创建job")
		newScaleJob := job.NewScaleJob(instance)
		err = r.client.Update(context.TODO(), newScaleJob)
		if err != nil {
			return reconcile.Result{}, err
		}

		//然后就去更新，更新要用retry操作去做
		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.client.Update(context.TODO(), found)
		})
		if retryErr != nil {
			return reconcile.Result{}, err //如果retry报错，就返回给下一次处理
		}


	}
	return reconcile.Result{}, nil
}

func toString(redisCluster *crdv1alpha1.RedisCluster) string {
	bytes, _ := json.Marshal(redisCluster.Spec)
	return  string(bytes)
}

func toSpec(data string) crdv1alpha1.RedisClusterSpec {
	redisClusterSpec := crdv1alpha1.RedisClusterSpec{}
	_ = json.Unmarshal([]byte(data), &redisClusterSpec)
	return redisClusterSpec
}

/*
// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(redisCluster *crdv1alpha1.RedisCluster) *corev1.Pod {
	labels := map[string]string{
		"app": redisCluster.Name,
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      redisCluster.Name + "-redis-trib-pod",
			Namespace: redisCluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "redis-trib",
					Image: redisCluster.Spec.RedisTribImage,
					ImagePullPolicy:corev1.PullIfNotPresent,
					Command:[]string{
						"/bin/bash",
						"-c",
						"/tmp/generate-script && /tmp/redis-trib.sh && tail -f /dev/null",
					},
					Env:[]corev1.EnvVar{
						//通过Sprintf把int32转换成了string
						{Name:"CLUSTER_SIZE",Value:fmt.Sprintf("%v",*redisCluster.Spec.Replicas)},
						{Name:"REDISCLUSTER_NAME",Value:redisCluster.Name},
						{Name:"NAMESPACE",Value:redisCluster.Namespace},

					},
				},
			},
		},
	}
 }
*/