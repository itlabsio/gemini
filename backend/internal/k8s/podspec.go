package k8s

import (
	"log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// defaultJobResources — requests/limits dump/restore-контейнера по умолчанию.
func defaultJobResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}
}

// DefaultWorkDirSize — размер тома дампа, когда ничего не задано (или задано криво).
const DefaultWorkDirSize = "20Gi"

// ValidQuantity проверяет, что строка парсится как k8s-величина (20Gi, 500m, 2).
// Используется валидаторами API, чтобы кривое значение не доехало до PodSpec.
func ValidQuantity(s string) error {
	_, err := resource.ParseQuantity(s)
	return err
}

// safeQuantity парсит величину, подставляя fallback вместо паники. resource.MustParse
// в этом файле раньше ронял процесс из фоновой горутины реконсилера, если в БД
// лежал невалидный storage_size (например "20GB" — опечатка в UI).
func safeQuantity(s, fallback string) resource.Quantity {
	if q, err := resource.ParseQuantity(s); err == nil {
		return q
	}
	if s != "" {
		log.Printf("k8s: invalid quantity %q, falling back to %s", s, fallback)
	}
	return resource.MustParse(fallback) // fallback — константа кода, не пользовательский ввод
}

// workVolume строит том /work: emptyDir или generic ephemeral PVC.
func workVolume(storageType, storageClass, size string) corev1.Volume {
	if size == "" {
		size = DefaultWorkDirSize
	}
	q := safeQuantity(size, DefaultWorkDirSize)
	if storageType == "emptydir" {
		return corev1.Volume{Name: "work", VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &q},
		}}
	}
	return corev1.Volume{Name: "work", VolumeSource: corev1.VolumeSource{Ephemeral: &corev1.EphemeralVolumeSource{
		VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: strPtrOrNil(storageClass),
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: q},
				},
			},
		},
	}}}
}

func boolPtr(b bool) *bool    { return &b }
func int32Ptr(i int32) *int32 { return &i }
func int64Ptr(i int64) *int64 { return &i }
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ptrQuantity — только для констант кода; для пользовательского ввода safeQuantity.
func ptrQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}
