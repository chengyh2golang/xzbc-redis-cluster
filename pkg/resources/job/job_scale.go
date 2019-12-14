package job

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"math/rand"
	"time"
	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"
)



func NewScaleJob(redisCluser *v1alpha1.RedisCluster,oldClusterSize,newClusterSize string)  *batchv1.Job {
	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: redisCluser.Name+"-job"+RandString(8),
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
							Name:    "redis-trib-scale",
							Image: redisCluser.Spec.RedisTribScaleImage,
							ImagePullPolicy:corev1.PullIfNotPresent,
							Command:[]string{
								"/bin/bash",
								"-c",
								//"/tmp/generate-scale-script && /tmp/redis-trib-scale.sh",
								"/tmp/generate-scale-script && /tmp/redis-trib-scale.sh",
								//"tail -f /dev/null",
							},

							Env:[]corev1.EnvVar{
								//通过Sprintf把int32转换成了string
								//{Name:"CLUSTER_SIZE",Value:fmt.Sprintf("%v",*redisCluser.Spec.Replicas)},
								{Name:"REDISCLUSTER_NAME",Value:redisCluser.Name},
								{Name:"NAMESPACE",Value:redisCluser.Namespace},
								{Name:"OLD_CLUSTER_SIZE",Value:oldClusterSize},
								{Name:"NEW_CLUSTER_SIZE",Value:newClusterSize},
								/*
								{
									Name: "OLD_CLUSTER_SIZE",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: redisCluser.Name+"-scale",
											},
											Key: "old_cluster_size",
										},
									},
								},
								{
									Name: "NEW_CLUSTER_SIZE",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: redisCluser.Name+"-scale",
											},
											Key: "new_cluster_size",
										},
									},
								},

								 */
							},

						},
					},
				},
			},
		},
	}

}

func RandString(len int) string {
	r := rand.New(rand.NewSource(time.Now().Unix()))
	bytes := make([]byte, len)
	for i := 0; i < len; i++ {
		b := r.Intn(26) + 65
		bytes[i] = byte(b)
	}
	return string(bytes)
}