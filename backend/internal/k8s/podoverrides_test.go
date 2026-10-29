package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func qty(rl corev1.ResourceList, name corev1.ResourceName) string {
	v, ok := rl[name]
	if !ok {
		return ""
	}
	return v.String()
}

func TestWithResourcesMergesPerField(t *testing.T) {
	// Без чарта — поверх встроенных дефолтов: задан только limits.memory.
	rr := PodOverrides{}.WithResources("", "", "", "8Gi").ContainerResources()
	if qty(rr.Requests, corev1.ResourceCPU) != "500m" || qty(rr.Requests, corev1.ResourceMemory) != "512Mi" ||
		qty(rr.Limits, corev1.ResourceCPU) != "2" || qty(rr.Limits, corev1.ResourceMemory) != "8Gi" {
		t.Fatalf("merge over built-in defaults: %+v", rr)
	}

	// Поверх чарта: чужие ресурсы (ephemeral-storage) сохраняются, чарт не мутирует.
	chart, err := ParsePodOverrides(`{"resources":{"requests":{"cpu":"1","ephemeral-storage":"1Gi"},"limits":{"memory":"4Gi"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	rr = chart.WithResources("", "1Gi", "", "").ContainerResources()
	if qty(rr.Requests, corev1.ResourceCPU) != "1" || qty(rr.Requests, corev1.ResourceMemory) != "1Gi" ||
		qty(rr.Requests, corev1.ResourceEphemeralStorage) != "1Gi" || qty(rr.Limits, corev1.ResourceMemory) != "4Gi" {
		t.Fatalf("merge over chart: %+v", rr)
	}
	if _, ok := chart.Resources.Requests[corev1.ResourceMemory]; ok {
		t.Fatal("chart overrides mutated")
	}

	// Всё пусто — ресурсы чарта как есть.
	if got := chart.WithResources("", "", "", ""); got.Resources != chart.Resources {
		t.Fatal("empty settings must keep chart resources")
	}
}

func TestValidateResources(t *testing.T) {
	if err := (PodOverrides{}).ValidateResources(); err != nil {
		t.Fatalf("built-in defaults: %v", err)
	}
	// requests.memory 4Gi поверх встроенного limits.memory 2Gi.
	if err := (PodOverrides{}).WithResources("", "4Gi", "", "").ValidateResources(); err == nil {
		t.Fatal("requests above limits must be rejected")
	}
}
