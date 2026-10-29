package k8s

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// PodOverrides — планирование и ресурсы dump/restore-подов, приходят из values
// Helm-чарта через env JOB_POD_OVERRIDES (JSON). Применяются и к Job, и к CronJob.
type PodOverrides struct {
	NodeSelector map[string]string            `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration          `json:"tolerations,omitempty"`
	Affinity     *corev1.Affinity             `json:"affinity,omitempty"`
	Resources    *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ParsePodOverrides разбирает JSON (пустая строка → нулевые overrides).
func ParsePodOverrides(s string) (PodOverrides, error) {
	var o PodOverrides
	if s == "" {
		return o, nil
	}
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		return o, fmt.Errorf("parse JOB_POD_OVERRIDES: %w", err)
	}
	return o, nil
}

// MergeScheduling накладывает nodeSelector/tolerations/affinity из other
// (непустые значения перекрывают). Resources не трогает.
func (o PodOverrides) MergeScheduling(other PodOverrides) PodOverrides {
	if len(other.NodeSelector) > 0 {
		o.NodeSelector = other.NodeSelector
	}
	if len(other.Tolerations) > 0 {
		o.Tolerations = other.Tolerations
	}
	if other.Affinity != nil {
		o.Affinity = other.Affinity
	}
	return o
}

// applyToPod проставляет nodeSelector/tolerations/affinity в PodSpec.
func (o PodOverrides) applyToPod(spec *corev1.PodSpec) {
	if len(o.NodeSelector) > 0 {
		spec.NodeSelector = o.NodeSelector
	}
	if len(o.Tolerations) > 0 {
		spec.Tolerations = o.Tolerations
	}
	if o.Affinity != nil {
		spec.Affinity = o.Affinity
	}
}

// ContainerResources возвращает ResourceRequirements: override или дефолт.
func (o PodOverrides) ContainerResources() corev1.ResourceRequirements {
	if o.Resources != nil {
		return *o.Resources
	}
	return defaultJobResources()
}

// WithResources возвращает копию overrides, где requests/limits из строковых
// значений накладываются поверх действующих ресурсов (чарта или встроенных
// дефолтов) поштучно: пустое значение оставляет то, что было.
func (o PodOverrides) WithResources(reqCPU, reqMem, limCPU, limMem string) PodOverrides {
	req := quantityList(reqCPU, reqMem)
	lim := quantityList(limCPU, limMem)
	if req == nil && lim == nil {
		return o
	}
	base := o.ContainerResources()
	o.Resources = &corev1.ResourceRequirements{
		Requests: mergeResourceList(base.Requests, req),
		Limits:   mergeResourceList(base.Limits, lim),
	}
	return o
}

// mergeResourceList — копия base с перекрытыми значениями из over (base не меняется).
func mergeResourceList(base, over corev1.ResourceList) corev1.ResourceList {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := make(corev1.ResourceList, len(base)+len(over))
	for k, v := range base {
		out[k] = v.DeepCopy()
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// ValidateResources проверяет, что requests не превышают limits — иначе k8s
// отклонит под Job'а.
func (o PodOverrides) ValidateResources() error {
	rr := o.ContainerResources()
	for name, req := range rr.Requests {
		if lim, ok := rr.Limits[name]; ok && req.Cmp(lim) > 0 {
			return fmt.Errorf("requests.%s (%s) exceeds limits.%s (%s)", name, req.String(), name, lim.String())
		}
	}
	return nil
}

func quantityList(cpu, mem string) corev1.ResourceList {
	rl := corev1.ResourceList{}
	if cpu != "" {
		if q, err := resource.ParseQuantity(cpu); err == nil {
			rl[corev1.ResourceCPU] = q
		}
	}
	if mem != "" {
		if q, err := resource.ParseQuantity(mem); err == nil {
			rl[corev1.ResourceMemory] = q
		}
	}
	if len(rl) == 0 {
		return nil
	}
	return rl
}
