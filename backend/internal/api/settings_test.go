package api

import (
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

func TestResourceSpecOf(t *testing.T) {
	want := model.ResourceSpec{
		Requests: model.ResourceQuantities{CPU: "500m", Memory: "512Mi"},
		Limits:   model.ResourceQuantities{CPU: "2", Memory: "2Gi"},
	}
	if got := resourceSpecOf(k8s.PodOverrides{}.ContainerResources()); got != want {
		t.Fatalf("built-in defaults: got %+v", got)
	}

	o, err := k8s.ParsePodOverrides(`{"resources":{"limits":{"memory":"8Gi"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	want = model.ResourceSpec{Limits: model.ResourceQuantities{Memory: "8Gi"}}
	if got := resourceSpecOf(o.ContainerResources()); got != want {
		t.Fatalf("chart resources: got %+v", got)
	}
}
