// Package push delivers family notifications through APNs: a child's channel
// request to the parents who can act on it, and the decision back to the
// child's devices. Delivery is best effort and never blocks the request that
// caused it.
package push

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// sendTimeout bounds one delivery attempt so a slow Apple endpoint cannot
// accumulate goroutines behind it.
const sendTimeout = 30 * time.Second

// Notification is one alert to one app audience.
type Notification struct {
	Title string
	Body  string

	// Kind travels in the payload so the receiving app knows what to refresh
	// without parsing display text.
	Kind string
}

// Kinds the apps switch on.
const (
	KindChannelRequested = "channel_requested"
	KindRequestDecided   = "request_decided"
)

// Sender delivers one notification to one device token. gone reports that
// Apple considers the token dead and it should be pruned.
type Sender interface {
	Send(ctx context.Context, deviceToken, bundleID string, n Notification) (gone bool, err error)
}

// TokenStore is the slice of the accounts store the service needs.
type TokenStore interface {
	ChildPushTokens(ctx context.Context, childID uuid.UUID) ([]string, error)
	ParentPushTokensForChild(ctx context.Context, familyID, childID uuid.UUID) ([]string, error)
	PrunePushTokens(ctx context.Context, tokens []string) error
}

// Service fans notifications out to the right registrations. A nil *Service is
// valid and does nothing, which is how a deployment without APNs runs.
type Service struct {
	tokens         TokenStore
	sender         Sender
	parentBundleID string
	childBundleID  string
	logger         *slog.Logger

	// wg lets tests and shutdown wait for in-flight deliveries.
	wg sync.WaitGroup
}

// New builds the service.
func New(tokens TokenStore, sender Sender, parentBundleID, childBundleID string,
	logger *slog.Logger) *Service {

	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		tokens:         tokens,
		sender:         sender,
		parentBundleID: parentBundleID,
		childBundleID:  childBundleID,
		logger:         logger,
	}
}

// ChannelRequested notifies every parent who can act on the child's request.
func (s *Service) ChannelRequested(familyID, childID uuid.UUID, childName, channelTitle string) {
	if s == nil {
		return
	}
	s.deliver(func(ctx context.Context) ([]string, error) {
		return s.tokens.ParentPushTokensForChild(ctx, familyID, childID)
	}, s.parentBundleID, Notification{
		Title: "New channel request",
		Body:  childName + " asked to watch " + channelTitle + ".",
		Kind:  KindChannelRequested,
	})
}

// RequestDecided notifies the child's devices of a parent's decision.
func (s *Service) RequestDecided(childID uuid.UUID, channelTitle string, approved bool) {
	if s == nil {
		return
	}
	notification := Notification{
		Title: "You got a yes!",
		Body:  channelTitle + " is ready to watch.",
		Kind:  KindRequestDecided,
	}
	if !approved {
		notification.Title = "About your request"
		notification.Body = "Your grown-up said not right now to " + channelTitle + "."
	}
	s.deliver(func(ctx context.Context) ([]string, error) {
		return s.tokens.ChildPushTokens(ctx, childID)
	}, s.childBundleID, notification)
}

// Wait blocks until in-flight deliveries finish. For shutdown and tests.
func (s *Service) Wait() {
	if s == nil {
		return
	}
	s.wg.Wait()
}

// deliver resolves recipients and sends in the background. The caller's
// request context is deliberately not used: the notification should still go
// out after the triggering response is written.
func (s *Service) deliver(recipients func(context.Context) ([]string, error),
	bundleID string, n Notification) {

	s.wg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()

		tokens, err := recipients(ctx)
		if err != nil {
			s.logger.Error("push: resolving recipients", "error", err)
			return
		}

		var dead []string
		for _, token := range tokens {
			gone, err := s.sender.Send(ctx, token, bundleID, n)
			if gone {
				dead = append(dead, token)
				continue
			}
			if err != nil {
				s.logger.Error("push: sending", "error", err)
			}
		}
		if err := s.tokens.PrunePushTokens(ctx, dead); err != nil {
			s.logger.Error("push: pruning dead tokens", "error", err)
		}
	})
}
