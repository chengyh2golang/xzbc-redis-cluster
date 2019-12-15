package job

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"
)

func NewScaleJob(redisCluser *v1alpha1.RedisCluster,oldClusterSize,newClusterSize string)  *batchv1.Job {
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
							Name:    "redis-trib-scale",
							Image: redisCluser.Spec.RedisTribImage,
							ImagePullPolicy:corev1.PullIfNotPresent,
							Command:[]string{
								"/bin/bash",
								"-c",
								//"/tmp/generate-script && tail -f /dev/null",
								"/tmp/generate-script && /tmp/redis-trib-scale.sh",
							},
							Env:[]corev1.EnvVar{
								{Name:"REDISCLUSTER_NAME",Value:redisCluser.Name},
								{Name:"CLUSTER_OP_TYPE",Value:"scale"},
								{Name:"NAMESPACE",Value:redisCluser.Namespace},
								{Name:"OLD_CLUSTER_SIZE",Value:oldClusterSize},
								{Name:"NEW_CLUSTER_SIZE",Value:newClusterSize},
							},
						},
					},
				},
			},
		},
	}
}

