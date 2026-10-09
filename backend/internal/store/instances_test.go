package store

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/secretcrypto"
)

func testStore(t *testing.T) (*Store, context.Context) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN не задан — тест требует живой Postgres с накатанными миграциями")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	box, err := secretcrypto.New("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	return New(pool, box), ctx
}

func TestInstanceCredentialsRoundTrip(t *testing.T) {
	s, ctx := testStore(t)

	// --- create (plain) ---
	inst, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceSource, Name: "rt-test", Host: "db.example", Port: 6432,
		SSLMode: "verify-full", DiscoveryDB: "app",
		ExcludedDatabases: []string{"audit", "scratch"},
		AuthType:          model.AuthPlain, PlainUsername: "u1", PlainPassword: "s3cret",
	}, "tester@example.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteInstance(ctx, inst.ID) })
	if inst.AuthType != model.AuthPlain || inst.PlainUsername != "u1" {
		t.Fatalf("join не подтянул дескриптор кред: %+v", inst)
	}
	if !slices.Equal(inst.ExcludedDatabases, []string{"audit", "scratch"}) {
		t.Fatalf("excluded_databases не сохранились: %+v", inst.ExcludedDatabases)
	}
	t.Logf("create -> auth_type=%s plain_username=%s (join работает)", inst.AuthType, inst.PlainUsername)

	c, err := s.Credentials(ctx, inst.ID)
	if err != nil || c.Password != "s3cret" {
		t.Fatalf("creds после create: %+v err=%v", c, err)
	}
	t.Logf("credentials -> пароль расшифрован верно")

	// --- update с ПУСТЫМ паролем: должен сохраниться прежний ---
	if _, err := s.UpdateInstance(ctx, inst.ID, InstanceInput{
		Role: model.InstanceSource, Name: "rt-test", Host: "db2.example", Port: 6432,
		SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u1", PlainPassword: "",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	c, err = s.Credentials(ctx, inst.ID)
	if err != nil || c.Password != "s3cret" {
		t.Fatalf("пароль потерян при пустом вводе: %+v err=%v", c, err)
	}
	t.Logf("update с пустым паролем -> прежний пароль сохранён")

	// --- переключение на vault с маппингом ключей ---
	got, err := s.UpdateInstance(ctx, inst.ID, InstanceInput{
		Role: model.InstanceSource, Name: "rt-test", Host: "db2.example", Port: 6432,
		SSLMode: "require", AuthType: model.AuthVault,
		VaultPath: "kv/data/x/y", VaultRole: "gemini",
		VaultUsernameKey: "db_login", VaultPasswordKey: "db_secret",
	})
	if err != nil {
		t.Fatalf("update->vault: %v", err)
	}
	if got.VaultUsernameKey != "db_login" || got.VaultPasswordKey != "db_secret" {
		t.Fatalf("маппинг ключей не сохранился: %+v", got)
	}
	if len(got.ExcludedDatabases) != 0 {
		t.Fatalf("update без excluded_databases должен очищать список, получили %+v", got.ExcludedDatabases)
	}
	c, _ = s.Credentials(ctx, inst.ID)
	t.Logf("switch->vault -> path=%s ukey=%s pkey=%s", c.VaultPath, c.VaultUsernameKey, c.VaultPasswordKey)

	// --- каскад: удаление инстанса уносит креды ---
	if err := s.DeleteInstance(ctx, inst.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Credentials(ctx, inst.ID); err == nil {
		t.Fatal("креды пережили удаление инстанса — каскад не сработал")
	}
	t.Logf("delete -> креды удалены каскадом")

	// --- CHECK: неполная vault-запись должна отвергаться БД ---
	if _, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceSource, Name: "rt-bad", Host: "h", Port: 5432,
		AuthType: model.AuthVault, VaultPath: "",
	}, "tester"); err == nil {
		_ = s.DeleteInstance(ctx, "rt-bad")
		t.Fatal("БД приняла vault-инстанс без vault_path — CHECK не работает")
	} else {
		t.Logf("CHECK creds_vault_complete -> отклонено: %v", err)
	}
}
