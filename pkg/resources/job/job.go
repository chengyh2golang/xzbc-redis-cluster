package job

import (
	"fmt"

	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func New(redisCluser *v1alpha1.RedisCluster)  *batchv1.Job {
	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: redisCluser.Name + "-job-" + RandString(8),
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
					RestartPolicy:corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "redis-trib-create",
							Image: redisCluser.Spec.RedisTribImage,
							ImagePullPolicy:corev1.PullIfNotPresent,
							Command:[]string{
								"/bin/bash",
								"-c",
								"/tmp/generate-script && /tmp/redis-trib-create.sh",
							},
							Env:[]corev1.EnvVar{
								//通过Sprintf把int32转换成了string
								{Name:"CLUSTER_SIZE",Value:fmt.Sprintf("%v",*redisCluser.Spec.Replicas)},
								{Name:"REDISCLUSTER_NAME",Value:redisCluser.Name},
								{Name:"CLUSTER_OP_TYPE",Value:"create"},
								{Name:"NAMESPACE",Value:redisCluser.Namespace},
							},
						},
					},
				},
			},
		},
	}
}
