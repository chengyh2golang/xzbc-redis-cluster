package configmap

import (
	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime/schema"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewScaleConfigMap(redisCluster *v1alpha1.RedisCluster,oldClusterSize, newClusterSize string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta:metav1.ObjectMeta{
			Name:redisCluster.Name+"-scale",
			Namespace:redisCluster.Namespace,
			OwnerReferences:[]metav1.OwnerReference{
				*metav1.NewControllerRef(redisCluster,schema.GroupVersionKind{
					Group:v1alpha1.SchemeGroupVersion.Group,
					Version:v1alpha1.SchemeGroupVersion.Version,
					Kind:"RedisCluster",
				}),
			},
			Labels:map[string]string{"crd.xzbc.com.cn": redisCluster.Name+"-scale"},
		},
		Data: map[string]string{
			"old_cluster_size":oldClusterSize,
			"new_cluster_size":newClusterSize,
		},
	}
}

