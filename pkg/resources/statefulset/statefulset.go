package statefulset

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"xzbc-redis-cluster/pkg/apis/crd/v1alpha1"
)

const (
	RedisConfigKey          = "redis.conf"
	RedisConfigRelativePath = "redis.conf"
)

func New(redisCluster *v1alpha1.RedisCluster) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Statefulset",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      redisCluster.Name,
			Namespace: redisCluster.Namespace,
			Labels:    map[string]string{"crd.xzbc.com.cn": redisCluster.Name},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(redisCluster, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "RedisCluster",
				}),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			//这个service是headless的svc
			ServiceName: redisCluster.Name,
			Replicas:    redisCluster.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"crd.xzbc.com.cn/v1alpha1": redisCluster.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: redisCluster.Name,
					Labels: map[string]string{
						"crd.xzbc.com.cn/v1alpha1": redisCluster.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "rediscluster", //现在是硬编码
							Image:           redisCluster.Spec.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Resources:       redisCluster.Spec.Resources,
							//redis里port有多个，6379用于服务监听, 用于集群通信的16379
							Ports: []corev1.ContainerPort{
								{Name: "redis", ContainerPort: 6379,},
								{Name: "cluster", ContainerPort: 16379,},
							},
							Env: []corev1.EnvVar{
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "redis-conf", MountPath: "/etc/redis"},
								{Name: "redis-data", MountPath: "/data"},
							},
							Command: []string{
								"redis-server",
								"/etc/redis/redis.conf",
								"--protected-mode no",
							},
						},
					},
					Volumes: []corev1.Volume{
						/*
						{

							Name: "redis-data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						*/
						{
							Name: "redis-conf",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									Items: []corev1.KeyToPath{
										{Key: RedisConfigKey, Path: RedisConfigRelativePath},
									},
									LocalObjectReference: corev1.LocalObjectReference{
										Name: redisCluster.Name,
									},
								},
							},
						},
					},
				},
			},
			// 如果需要在本地测试，没有共享存储环境，需要使用emptyDir{}
			// 可以先注释掉下面这段VolumeClaimTemplates代码
				VolumeClaimTemplates:[]corev1.PersistentVolumeClaim{
					{
						ObjectMeta:metav1.ObjectMeta{
							Name:"redis-data",

						},
						Spec:corev1.PersistentVolumeClaimSpec{
							AccessModes:[]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							StorageClassName:&redisCluster.Spec.StorageClassName,
							Resources:corev1.ResourceRequirements{
								Requests:corev1.ResourceList{
									corev1.ResourceStorage:resource.MustParse(
										//cr里定义的storage的格式需要是"5Gi"，string类型
									redisCluster.Spec.Storage),
								},
							},
						},
					},
				},
		},
	}
}