// Package k8s создаёт и следит за dump/restore-Job'ами через прямой CRUD в
// batch/v1 (без собственных CRD). Также управляет ephemeral-секретами кред.
package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Labels, которыми Gemini метит все свои объекты.
const (
	LabelManagedBy  = "app.kubernetes.io/managed-by"
	LabelName       = "app.kubernetes.io/name"
	LabelPartOf     = "app.kubernetes.io/part-of"
	LabelInstance   = "app.kubernetes.io/instance" // релиз головы — для дерева Argo CD
	LabelComponent  = "gemini.itlabs.io/component" // "dump" | "restore" | "credentials"
	LabelRunID      = "gemini.itlabs.io/run-id"
	LabelDatabaseID = "gemini.itlabs.io/database-id"
	LabelInstanceID = "gemini.itlabs.io/instance-id"
	LabelTrigger    = "gemini.itlabs.io/trigger" // "manual" | "scheduled" (на Job'е)
	ManagedByValue  = "gemini"
)

// Client — обёртка над clientset под фиксированный namespace.
type Client struct {
	cs        kubernetes.Interface
	namespace string

	// Проставляются во все создаваемые объекты (Job / CronJob / ephemeral Secret).
	// Заполняются из Deployment'а backend'а в ResolveOwner.
	instanceLabel    string
	imagePullSecrets []string
	imagePullPolicy  corev1.PullPolicy
	ownerRef         *metav1.OwnerReference
}

// ResolveOwner читает Deployment backend'а и запоминает:
//   - его как ownerReference — тогда Argo CD рисует Job/CronJob в дереве релиза,
//     а удаление релиза каскадно чистит их;
//   - его imagePullSecrets — те же креды для pull образа dump/restore-подов;
//   - его imagePullPolicy — чтобы dump/restore-Job'ы тянули тот же образ по тем
//     же правилам (иначе при :latest + PullAlways у backend'а Job'ы застревают
//     на закешированном старом образе);
//   - его app.kubernetes.io/instance — лейбл релиза на создаваемых объектах.
//
// Best-effort: при ошибке ничего не проставляется (Job'ы всё равно создаются).
func (c *Client) ResolveOwner(ctx context.Context, deploymentName string) error {
	if deploymentName == "" {
		return nil
	}
	d, err := c.cs.AppsV1().Deployments(c.namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("resolve owner deployment %q: %w", deploymentName, err)
	}
	c.ownerRef = &metav1.OwnerReference{
		APIVersion:         "apps/v1",
		Kind:               "Deployment",
		Name:               d.Name,
		UID:                d.UID,
		Controller:         boolPtr(false),
		BlockOwnerDeletion: boolPtr(false),
	}
	c.instanceLabel = d.Labels[LabelInstance]
	for _, ref := range d.Spec.Template.Spec.ImagePullSecrets {
		c.imagePullSecrets = append(c.imagePullSecrets, ref.Name)
	}
	if cs := d.Spec.Template.Spec.Containers; len(cs) > 0 {
		c.imagePullPolicy = cs[0].ImagePullPolicy
	}
	return nil
}

// ownerRefs — срез для ObjectMeta.OwnerReferences (nil, пока ResolveOwner не вызван).
func (c *Client) ownerRefs() []metav1.OwnerReference {
	if c.ownerRef == nil {
		return nil
	}
	return []metav1.OwnerReference{*c.ownerRef}
}

// New создаёт клиента: сначала пытается in-cluster config, затем ~/.kube/config
// (или $KUBECONFIG) — удобно для локального запуска.
func New(namespace string) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			if home, herr := os.UserHomeDir(); herr == nil {
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("k8s config (in-cluster and kubeconfig both failed): %w", err)
		}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s clientset: %w", err)
	}
	return &Client{cs: cs, namespace: namespace}, nil
}

// Namespace отдаёт целевой namespace.
func (c *Client) Namespace() string { return c.namespace }

// ownerLabels — общий набор лейблов для объектов прогона. instance (релиз головы)
// добавляется как app.kubernetes.io/instance, чтобы объекты были видны в дереве Argo CD.
func ownerLabels(component, runID, databaseID, instance string) map[string]string {
	l := map[string]string{
		LabelManagedBy: ManagedByValue,
		LabelName:      ManagedByValue,
		LabelPartOf:    ManagedByValue,
		LabelComponent: component,
	}
	if runID != "" {
		l[LabelRunID] = runID
	}
	if databaseID != "" {
		l[LabelDatabaseID] = databaseID
	}
	if instance != "" {
		l[LabelInstance] = instance
	}
	return l
}

// pullSecretRefs преобразует имена в []LocalObjectReference (nil при пустом списке).
func pullSecretRefs(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(names))
	for _, n := range names {
		refs = append(refs, corev1.LocalObjectReference{Name: n})
	}
	return refs
}

func objectMeta(name string, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Labels: labels}
}
