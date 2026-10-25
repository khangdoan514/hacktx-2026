package extensions

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
)

type policyContextKey struct{}

type callSpec struct {
	Method   string
	URL      string
	Policy   NetworkPolicy
	Body     []byte
	Headers  map[string]string
	MaxBytes int
	Class    Class
	Side     SideEffect
}

type httpResult struct {
	Status int
	Body   []byte
}

type callFailure struct {
	errorCode ErrorCode
	message   string
	crash     bool
	uncertain bool
	retryable bool
	terminal  bool
}

func (f *callFailure) Error() string {
	if f == nil {
		return ""
	}
	return string(f.errorCode) + ": " + f.message
}

func newSafeClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fail(CodeNetworkDenied, "redirects are not allowed")
		},
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				policy, _ := ctx.Value(policyContextKey{}).(NetworkPolicy)
				if err := AllowTarget(policy, addr); err != nil {
					return nil, err
				}
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, addr)
			},
			ForceAttemptHTTP2: false,
			DisableKeepAlives: true,
			TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		},
	}
}

func doCall(ctx context.Context, client *http.Client, spec callSpec) (httpResult, error) {
	if err := AllowTarget(spec.Policy, spec.URL); err != nil {
		return httpResult{}, err
	}
	if client == nil {
		client = newSafeClient()
	}
	ctx = context.WithValue(ctx, policyContextKey{}, spec.Policy)
	req, err := http.NewRequestWithContext(ctx, spec.Method, spec.URL, bytes.NewReader(spec.Body))
	if err != nil {
		return httpResult{}, fail(CodeInvalidInput, "tool endpoint is invalid")
	}
	for key, value := range spec.Headers {
		if strings.EqualFold(key, "Authorization") && strings.ContainsAny(value, "\r\n") {
			return httpResult{}, fail(CodeCredentialDenied, "credential value is not allowed")
		}
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return httpResult{}, classifyTransport(spec.Class, spec.Side, err)
	}
	defer resp.Body.Close()
	limit := spec.MaxBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	body, err := readLimited(resp.Body, limit, spec)
	if err != nil {
		return httpResult{}, err
	}
	return httpResult{Status: resp.StatusCode, Body: body}, nil
}

func readLimited(r io.Reader, limit int, spec callSpec) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, classifyTransport(spec.Class, spec.Side, err)
	}
	if len(buf) > limit {
		return nil, &callFailure{
			errorCode: CodeResourceLimit,
			message:   "tool output exceeded the size limit",
			terminal:  spec.Side == SideEffectWrite,
		}
	}
	return buf, nil
}

func classifyTransport(class Class, side SideEffect, err error) *callFailure {
	var typed *Error
	if errors.As(err, &typed) {
		return &callFailure{errorCode: typed.Code, message: typed.Message}
	}
	if errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return &callFailure{
			errorCode: CodeCancelled,
			message:   "tool call cancelled",
			uncertain: side == SideEffectWrite,
			terminal:  side == SideEffectWrite,
		}
	}
	if isTimeout(err) {
		return &callFailure{
			errorCode: CodeTimeout,
			message:   "tool timed out",
			retryable: side == SideEffectReadOnly,
			uncertain: side == SideEffectWrite,
			terminal:  side == SideEffectWrite,
		}
	}
	refused := errors.Is(err, syscall.ECONNREFUSED)
	failure := &callFailure{retryable: side == SideEffectReadOnly}
	if class == ClassWorker {
		failure.errorCode = CodeWorkerCrash
		failure.message = "extension worker crashed"
		failure.crash = true
	} else {
		failure.errorCode = CodeUpstreamUnavailable
		failure.message = "tool service is unavailable"
	}
	if side == SideEffectWrite && !refused {
		failure.uncertain = true
		failure.terminal = true
	}
	return failure
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func classifyStatus(class Class, side SideEffect, status int) error {
	if status >= 200 && status < 300 {
		return nil
	}
	_ = class
	failure := &callFailure{errorCode: CodeInternal, message: "tool request was rejected"}
	switch {
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		failure.errorCode = CodeTimeout
		failure.message = "tool timed out"
		failure.retryable = side == SideEffectReadOnly
		failure.uncertain = side == SideEffectWrite
		failure.terminal = side == SideEffectWrite
	case status >= 500:
		failure.errorCode = CodeUpstreamUnavailable
		failure.message = "tool service failed"
		failure.retryable = side == SideEffectReadOnly
		failure.uncertain = side == SideEffectWrite
		failure.terminal = true
	default:
		failure.terminal = side == SideEffectWrite
	}
	return failure
}

func correlationHeaders(d Dispatch) map[string]string {
	return map[string]string{
		"Content-Type":       "application/json",
		"Accept":             "application/json",
		"NMC3-Run-Id":        d.RunID,
		"NMC3-Invocation-Id": d.InvocationID,
		"NMC3-Tenant-Id":     d.Tool.TenantID,
		"NMC3-Tool-Id":       d.Tool.ID,
		"NMC3-Tool-Version":  d.Tool.Version,
		"NMC3-Principal-Id":  d.PrincipalID,
	}
}

func bearer(headers map[string]string, cred Credential, present bool) error {
	if !present {
		return nil
	}
	if strings.ContainsAny(cred.Value, "\r\n") || cred.Value == "" {
		return fail(CodeCredentialDenied, "credential value is not allowed")
	}
	headers["Authorization"] = "Bearer " + cred.Value
	return nil
}

func joinPath(base, path string) (string, error) {
	parsed, err := parseEndpoint(base)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return nil, fail(CodeInvalidInput, "tool endpoint is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fail(CodeNetworkDenied, "tool URL scheme is not allowed")
	}
	return parsed, nil
}
