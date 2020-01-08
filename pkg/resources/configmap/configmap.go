package configmap

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RedisConfigKey          = "redis.conf"
	FixIPKey = "fix-ip.sh"
	//RedisConfigRelativePath = "redis.conf"
)

var redisConfig = `cluster-enabled yes
cluster-config-file /data/nodes.conf
cluster-node-timeout 5000
cluster-migration-barrier 1
dir /data
appendonly yes
protected-mode no
`

var fixIPConfig = `#!/bin/sh
LATEST_IP_FILE="/data/latest-ip"
if [ -f ${LATEST_IP_FILE} ]; then
  if [ $POD_IP ] && [ $(cat ${LATEST_IP_FILE}) != $POD_IP ];then
    sed -i "s/$(cat ${LATEST_IP_FILE})/${POD_IP}/g" /data/nodes.conf
    echo $POD_IP > ${LATEST_IP_FILE}
  fi
else
  echo $POD_IP > $LATEST_IP_FILE
fi
`

func New(redisCluster *v1alpha1.RedisCluster) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:metav1.TypeMeta{
			Kind:       "ConfigMap",
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
			Labels:map[string]string{"crd.xzbc.com.cn": redisCluster.Name},
		},
		Data: map[string]string{
			RedisConfigKey:redisConfig,
			FixIPKey:fixIPConfig,
		},
	}
}

