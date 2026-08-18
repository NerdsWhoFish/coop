package push

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type fakeStore struct {
	mu           sync.Mutex
	childTokens  []string
	parentTokens []string
	pruned       []string
}

func (f *fakeStore) ChildPushTokens(context.Context, uuid.UUID) ([]string, error) {
	return f.childTokens, nil
}

func (f *fakeStore) ParentPushTokensForChild(context.Context, uuid.UUID, uuid.UUID) ([]string, error) {
	return f.parentTokens, nil
}

func (f *fakeStore) PrunePushTokens(_ context.Context, tokens []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruned = append(f.pruned, tokens...)
	return nil
}

type fakeSender struct {
	mu   sync.Mutex
	sent []sentPush
	gone map[string]bool
}

type sentPush struct {
	token    string
	bundleID string
	n        Notification
}

func (f *fakeSender) Send(_ context.Context, token, bundleID string, n Notification) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentPush{token: token, bundleID: bundleID, n: n})
	return f.gone[token], nil
}

func TestChannelRequestedReachesEveryEligibleParent(t *testing.T) {
	store := &fakeStore{parentTokens: []string{"admin-phone", "scoped-phone"}}
	sender := &fakeSender{}
	service := New(store, sender, "parent.app", "child.app", nil)

	service.ChannelRequested(uuid.New(), uuid.New(), "Cooper", "Trains Galore")
	service.Wait()

	if len(sender.sent) != 2 {
		t.Fatalf("sent %d notifications, want 2", len(sender.sent))
	}
	for _, sent := range sender.sent {
		if sent.bundleID != "parent.app" {
			t.Fatalf("sent to bundle %q, want the parent app", sent.bundleID)
		}
		if sent.n.Kind != KindChannelRequested {
			t.Fatalf("kind = %q, want %q", sent.n.Kind, KindChannelRequested)
		}
	}
}

func TestRequestDecidedWordsDependOnDecision(t *testing.T) {
	store := &fakeStore{childTokens: []string{"kid-ipad"}}
	sender := &fakeSender{}
	service := New(store, sender, "parent.app", "child.app", nil)

	service.RequestDecided(uuid.New(), "Trains Galore", true)
	service.RequestDecided(uuid.New(), "Trains Galore", false)
	service.Wait()

	if len(sender.sent) != 2 {
		t.Fatalf("sent %d notifications, want 2", len(sender.sent))
	}
	bodies := []string{sender.sent[0].n.Body, sender.sent[1].n.Body}
	slices.Sort(bodies)
	if bodies[0] == bodies[1] {
		t.Fatal("approval and denial produced identical wording")
	}
	for _, sent := range sender.sent {
		if sent.bundleID != "child.app" {
			t.Fatalf("sent to bundle %q, want the child app", sent.bundleID)
		}
	}
}

func TestGoneTokensArePruned(t *testing.T) {
	store := &fakeStore{childTokens: []string{"dead", "alive"}}
	sender := &fakeSender{gone: map[string]bool{"dead": true}}
	service := New(store, sender, "parent.app", "child.app", nil)

	service.RequestDecided(uuid.New(), "Trains Galore", true)
	service.Wait()

	if !slices.Equal(store.pruned, []string{"dead"}) {
		t.Fatalf("pruned %v, want only the dead token", store.pruned)
	}
}

func TestNilServiceDeliversNothing(t *testing.T) {
	var service *Service
	service.ChannelRequested(uuid.New(), uuid.New(), "Cooper", "Trains Galore")
	service.RequestDecided(uuid.New(), "Trains Galore", true)
	service.Wait()
}
