package tcc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

type rapidRetryTransport struct {
	base http.RoundTripper
}

func (t rapidRetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	for i := 0; i < rapidRetryCount; i++ {
		attempt, cancel, err := cloneRequestWithTimeout(request, rapidRetryTimeout)
		if err != nil {
			return base.RoundTrip(request)
		}
		response, err := base.RoundTrip(attempt)
		if err == nil {
			if response.Body == nil {
				cancel()
			} else {
				response.Body = cancelOnCloseReadCloser{ReadCloser: response.Body, cancel: cancel}
			}
			return response, nil
		}
		cancel()
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if !isTimeoutError(err) {
			return nil, err
		}
		log.Printf("rapid retry: %s %s timed out after %s; starting attempt %d", request.Method, request.URL, rapidRetryTimeout, i+2)
	}
	return cloneAndRoundTrip(base, request)
}

func cloneRequestWithTimeout(request *http.Request, timeout time.Duration) (*http.Request, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	clone, err := cloneRequest(request)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return clone.WithContext(ctx), cancel, nil
}

func cloneAndRoundTrip(transport http.RoundTripper, request *http.Request) (*http.Response, error) {
	clone, err := cloneRequest(request)
	if err != nil {
		return nil, err
	}
	return transport.RoundTrip(clone)
}

func cloneRequest(request *http.Request) (*http.Request, error) {
	clone := request.Clone(request.Context())
	if request.Body == nil || request.Body == http.NoBody {
		return clone, nil
	}
	if request.GetBody == nil {
		return nil, fmt.Errorf("request body cannot be retried")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnCloseReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
