package auth

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
)

// deviceClient is what the device flow needs of the generated client.
type deviceClient interface {
	OidcDeviceAuthorizeWithResponse(ctx context.Context, reqEditors ...groundcontrol.RequestEditorFn) (*groundcontrol.OidcDeviceAuthorizeHTTPResponse, error)
	OidcDeviceTokenWithResponse(ctx context.Context, body groundcontrol.DeviceTokenRequest, reqEditors ...groundcontrol.RequestEditorFn) (*groundcontrol.OidcDeviceTokenHTTPResponse, error)
}

// deviceLogin runs the OIDC device flow (RFC 8628) through Ground Control:
// it shows the user code and polls at the interval Ground Control gave,
// 5 seconds more after each slow_down, until a session or the code's
// expiry.
func deviceLogin(ctx context.Context, client deviceClient, out io.Writer, sleep func(context.Context, time.Duration) error) (groundcontrol.LoginResponse, error) {
	start, err := client.OidcDeviceAuthorizeWithResponse(ctx)
	if err != nil {
		return groundcontrol.LoginResponse{}, err
	}
	if err := common.ResponseError(start); err != nil {
		return groundcontrol.LoginResponse{}, err
	}
	auth := start.JSON200
	if auth == nil {
		return groundcontrol.LoginResponse{}, fmt.Errorf("device authorization response is empty")
	}
	uri := auth.VerificationURI
	if auth.VerificationURIComplete != "" {
		uri = auth.VerificationURIComplete
	}
	_, _ = fmt.Fprintf(out, "Open %s and enter the code %s\n", uri, auth.UserCode)
	interval := time.Duration(max(auth.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second)
	for {
		if err := sleep(ctx, interval); err != nil {
			return groundcontrol.LoginResponse{}, err
		}
		if auth.ExpiresIn > 0 && time.Now().After(deadline) {
			return groundcontrol.LoginResponse{}, fmt.Errorf("the device code expired before the login finished")
		}
		poll, err := client.OidcDeviceTokenWithResponse(ctx, groundcontrol.DeviceTokenRequest{DeviceCode: auth.DeviceCode})
		if err != nil {
			return groundcontrol.LoginResponse{}, err
		}
		switch {
		case poll.JSON200 != nil:
			return *poll.JSON200, nil
		case poll.JSON400 != nil && poll.JSON400.Message == "authorization_pending":
			continue
		case poll.JSON400 != nil && poll.JSON400.Message == "slow_down":
			interval += 5 * time.Second
			continue
		}
		if err := common.ResponseError(poll); err != nil {
			return groundcontrol.LoginResponse{}, err
		}
		return groundcontrol.LoginResponse{}, fmt.Errorf("device token response is empty")
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
