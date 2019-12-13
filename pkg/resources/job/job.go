package job

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"
)

func New(redisCluser *v1alpha1.RedisCluster)  *batchv1.Job {
	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: redisCluser.Name,
			Namespace: redisCluser.Namespace,
			Labels:    map[string]string{"crd.xzbc.com.cn": redisCluser.Name},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(redisCluser, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "RedisCluster",
				}),
			},
		},
		Spec:batchv1.JobSpec{
			Template:corev1.PodTemplateSpec{

				Spec:corev1.PodSpec{
					RestartPolicy:corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "redis-trib",
							Image: redisCluser.Spec.RedisTribImage,
							ImagePullPolicy:corev1.PullIfNotPresent,
							Command:[]string{
								"/bin/bash",
								"-c",
								"/tmp/generate-script && /tmp/redis-trib.sh",
							},
							Env:[]corev1.EnvVar{
								//通过Sprintf把int32转换成了string
								{Name:"CLUSTER_SIZE",Value:fmt.Sprintf("%v",*redisCluser.Spec.Replicas)},
								{Name:"REDISCLUSTER_NAME",Value:redisCluser.Name},
								{Name:"NAMESPACE",Value:redisCluser.Namespace},

							},
						},
					},
				},
			},
		},
	}

}
