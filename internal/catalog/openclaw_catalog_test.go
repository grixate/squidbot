package catalog

import "testing"

func TestLookupHelpers(t *testing.T) {
	provider, ok := ProviderByID("openai")
	if !ok {
		t.Fatal("expected openai provider")
	}
	if provider.ID != "openai" {
		t.Fatalf("unexpected provider: %+v", provider)
	}
	if _, ok := ProviderByID("missing"); ok {
		t.Fatal("expected missing provider lookup to fail")
	}

	channel, ok := ChannelByID("telegram")
	if !ok {
		t.Fatal("expected telegram channel")
	}
	if channel.ID != "telegram" {
		t.Fatalf("unexpected channel: %+v", channel)
	}
	if _, ok := ChannelByID("missing"); ok {
		t.Fatal("expected missing channel lookup to fail")
	}
}

func TestCatalogInvariants(t *testing.T) {
	providerIDs := map[string]struct{}{}
	for _, provider := range OpenClawProviders {
		if provider.ID == "" {
			t.Fatalf("provider with empty id: %+v", provider)
		}
		if provider.Label == "" {
			t.Fatalf("provider with empty label: %+v", provider)
		}
		if _, exists := providerIDs[provider.ID]; exists {
			t.Fatalf("duplicate provider id: %s", provider.ID)
		}
		providerIDs[provider.ID] = struct{}{}
	}

	channelIDs := map[string]struct{}{}
	for _, channel := range OpenClawChannels {
		if channel.ID == "" {
			t.Fatalf("channel with empty id: %+v", channel)
		}
		if channel.Label == "" {
			t.Fatalf("channel with empty label: %+v", channel)
		}
		if _, exists := channelIDs[channel.ID]; exists {
			t.Fatalf("duplicate channel id: %s", channel.ID)
		}
		channelIDs[channel.ID] = struct{}{}
	}
}
