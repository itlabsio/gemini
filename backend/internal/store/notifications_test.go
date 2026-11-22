package store

import "testing"

// Прокси провайдера: наружу — без пароля, в канал — полный URL, пустой — удаление.
func TestNotificationProxy(t *testing.T) {
	s, ctx := testStore(t)
	const raw = "http://u:s3cret@proxy:3128"
	t.Cleanup(func() { _ = s.SetNotificationProxy(ctx, "telegram", "", "t") })
	if err := s.SetNotificationProxy(ctx, "telegram", raw, "t"); err != nil {
		t.Fatal(err)
	}
	shown, err := s.NotificationProxies(ctx)
	if err != nil || shown["telegram"] != "http://u:xxxxx@proxy:3128" {
		t.Fatalf("proxies = %v, %v", shown, err)
	}
	c, err := s.CreateNotificationChannel(ctx, "", NotificationChannelInput{Name: "px-test", Provider: "telegram",
		ChatID: "1", BotToken: "1:" + "aaaaaaaaaaaaaaaaaaaaaaaa", OnFailure: true, Enabled: true}, "t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteNotificationChannel(ctx, "", c.ID) })
	if ch, err := s.NotificationTarget(ctx, "", c.ID); err != nil || ch.Proxy != raw {
		t.Fatalf("target proxy = %q, %v", ch.Proxy, err)
	}
	if err := s.SetNotificationProxy(ctx, "telegram", "", "t"); err != nil {
		t.Fatal(err)
	}
	if ch, _ := s.NotificationTarget(ctx, "", c.ID); ch.Proxy != "" {
		t.Fatalf("proxy not removed: %q", ch.Proxy)
	}
	if p, err := s.NotificationProxy(ctx, "telegram"); err != nil || p != "" {
		t.Fatalf("NotificationProxy = %q, %v", p, err)
	}
}
