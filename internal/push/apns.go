package push

import (
	"context"
	"fmt"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

// APNS sends through Apple's push service using token authentication, which
// never expires and needs no certificate renewal.
type APNS struct {
	client *apns2.Client
}

// NewAPNS builds a sender from the family's .p8 auth key. production selects
// Apple's environment: Ad Hoc and App Store installs live on production.
func NewAPNS(keyPEM []byte, keyID, teamID string, production bool) (*APNS, error) {
	authKey, err := token.AuthKeyFromBytes(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing APNs auth key: %w", err)
	}

	client := apns2.NewTokenClient(&token.Token{
		AuthKey: authKey,
		KeyID:   keyID,
		TeamID:  teamID,
	})
	if production {
		client = client.Production()
	} else {
		client = client.Development()
	}
	return &APNS{client: client}, nil
}

// Send implements Sender.
func (a *APNS) Send(ctx context.Context, deviceToken, bundleID string, n Notification) (
	bool, error) {

	body := payload.NewPayload().
		AlertTitle(n.Title).
		AlertBody(n.Body).
		Sound("default").
		Custom("coop", map[string]string{"kind": n.Kind})

	response, err := a.client.PushWithContext(ctx, &apns2.Notification{
		DeviceToken: deviceToken,
		Topic:       bundleID,
		PushType:    apns2.PushTypeAlert,
		Payload:     body,
	})
	if err != nil {
		return false, fmt.Errorf("sending push: %w", err)
	}
	if response.Sent() {
		return false, nil
	}
	if response.Reason == apns2.ReasonUnregistered || response.Reason == apns2.ReasonBadDeviceToken {
		return true, nil
	}
	return false, fmt.Errorf("push rejected: %s", response.Reason)
}
