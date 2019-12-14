package service

import (
	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func New(redisCluster *v1alpha1.RedisCluster) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta:metav1.ObjectMeta{
			Name:redisCluster.Name,
			Namespace:redisCluster.Namespace,
			OwnerReferences:[]metav1.OwnerReference{
				*metav1.NewControllerRef(redisCluster,schema.GroupVersionKind{
					Group:v1alpha1.SchemeGroupVersion.Group,
					Version:v1alpha1.SchemeGroupVersion.Version,
					Kind:"RedisCluster",
				}),
			},
		},
		Spec:corev1.ServiceSpec{
			Ports:[]corev1.ServicePort{
				{
					Port: 6379,
					Name: "redis",
				},
				{
					Port: 16379,
					Name: "cluster",
				},
			},
			ClusterIP:corev1.ClusterIPNone,
			Selector: map[string]string{
				"crd.xzbc.com.cn/v1alpha1":redisCluster.Name,
			},
		},
	}
}

