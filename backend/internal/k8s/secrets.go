package k8s

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CredsSecretName — имя стабильного секрета кред подключения к инстансу.
// Один на инстанс (host/port/user/password/sslmode + S3_BUCKETS); имя базы
// прокидывается в Job'ы отдельной переменной PGDATABASE. Живёт, пока у инстанса
// есть активная база/сопоставление; на него ссылаются CronJob-шаблоны через envFrom.
func CredsSecretName(instanceID string) string {
	return "gemini-instance-creds-" + instanceID
}

// UpsertCredsSecret создаёт/обновляет секрет кред инстанса.
func (c *Client) UpsertCredsSecret(ctx context.Context, instanceID string, data map[string]string) (string, error) {
	name := CredsSecretName(instanceID)
	meta := objectMeta(name, ownerLabels("credentials", "", "", c.instanceLabel))
	meta.Labels[LabelInstanceID] = instanceID
	meta.OwnerReferences = c.ownerRefs()
	sec := &corev1.Secret{ObjectMeta: meta, StringData: data, Type: corev1.SecretTypeOpaque}

	_, err := c.cs.CoreV1().Secrets(c.namespace).Create(ctx, sec, metav1.CreateOptions{})
	switch {
	case err == nil:
		log.Printf("k8s: Secret %s created (instance=%s)", name, instanceID)
	case apierrors.IsAlreadyExists(err):
		if _, err = c.cs.CoreV1().Secrets(c.namespace).Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update secret %s: %w", name, err)
		}
	default:
		return "", fmt.Errorf("create secret %s: %w", name, err)
	}
	return name, nil
}

// DeleteCredsSecret удаляет секрет кред инстанса — best-effort.
func (c *Client) DeleteCredsSecret(ctx context.Context, instanceID string) error {
	name := CredsSecretName(instanceID)
	err := c.cs.CoreV1().Secrets(c.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		log.Printf("k8s: Secret %s deleted", name)
	}
	return err
}

// ListManagedCredsSecrets возвращает instanceID → имя всех секретов кред Gemini
// (для сверки при reconcile).
func (c *Client) ListManagedCredsSecrets(ctx context.Context) (map[string]string, error) {
	list, err := c.cs.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: LabelManagedBy + "=" + ManagedByValue + "," + LabelComponent + "=credentials",
	})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for i := range list.Items {
		s := &list.Items[i]
		if id := s.Labels[LabelInstanceID]; id != "" {
			out[id] = s.Name
		}
	}
	return out, nil
}
