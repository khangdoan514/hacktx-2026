package extensions

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// AllowTarget reports whether target is an allowed IP dial. target may be a
// URL or host:port. Names are denied so the dialer never resolves DNS.
func AllowTarget(policy NetworkPolicy, target string) error {
	host, port, err := splitTarget(target)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fail(CodeNetworkDenied, "network target must be an IP address")
	}
	canonical := ip.String()
	for _, entry := range policy.Allow {
		allowedHost, allowedPort, ok := splitAllow(entry)
		if !ok {
			continue
		}
		allowedIP := net.ParseIP(allowedHost)
		if allowedIP == nil || allowedIP.String() != canonical {
			continue
		}
		if allowedPort == "" || allowedPort == port {
			return nil
		}
	}
	return fail(CodeNetworkDenied, "network target is not allowed")
}

func splitTarget(target string) (string, string, error) {
	if strings.Contains(target, "://") {
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" {
			return "", "", fail(CodeNetworkDenied, "network target is invalid")
		}
		if parsed.User != nil {
			return "", "", fail(CodeInvalidInput, "credentials are not allowed in a tool URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", "", fail(CodeNetworkDenied, "tool URL scheme is not allowed")
		}
		port := parsed.Port()
		if port == "" {
			if parsed.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		return parsed.Hostname(), port, nil
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" || port == "" {
		return "", "", fail(CodeNetworkDenied, "network target is invalid")
	}
	return host, port, nil
}

func splitAllow(entry string) (string, string, bool) {
	if strings.Contains(entry, "://") {
		host, port, err := splitTarget(entry)
		if err != nil {
			return "", "", false
		}
		return host, port, true
	}
	host, port, err := net.SplitHostPort(entry)
	if err == nil {
		return host, port, host != "" && port != ""
	}
	if net.ParseIP(entry) != nil {
		return entry, "", true
	}
	return "", "", false
}

type isolation struct {
	network NetworkPolicy
	root    string
	cred    Credential
	hasCred bool
}

type isolationKey struct{}

func withIsolation(ctx context.Context, iso isolation) context.Context {
	return context.WithValue(ctx, isolationKey{}, iso)
}

func isolationFrom(ctx context.Context) isolation {
	iso, _ := ctx.Value(isolationKey{}).(isolation)
	return iso
}

// CredentialFromContext returns the single in-memory credential for this call.
// There is no lookup API for another tenant or another reference.
func CredentialFromContext(ctx context.Context) (Credential, bool) {
	iso := isolationFrom(ctx)
	if !iso.hasCred {
		return Credential{}, false
	}
	return iso.cred, true
}

// Dial opens an allowed TCP address. Other networks and forbidden targets
// fail before a connection is attempted.
func Dial(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fail(CodeNetworkDenied, "network target is not allowed")
	}
	if err := AllowTarget(isolationFrom(ctx).network, address); err != nil {
		return nil, err
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// ReadFile reads a file inside the call's filesystem root.
func ReadFile(ctx context.Context, path string) ([]byte, error) {
	root := isolationFrom(ctx).root
	if root == "" {
		return nil, fail(CodeFilesystemDenied, "filesystem access is not allowed")
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fail(CodeFilesystemDenied, "filesystem root is not available")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootEval, path)
	}
	cleaned := filepath.Clean(path)
	if outside(rootEval, cleaned) {
		return nil, fail(CodeFilesystemDenied, "path is outside the worker filesystem root")
	}
	evaluated, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		if outside(rootEval, cleaned) {
			return nil, fail(CodeFilesystemDenied, "path is outside the worker filesystem root")
		}
		return nil, fail(CodeNotFound, "file not found")
	}
	if outside(rootEval, evaluated) {
		return nil, fail(CodeFilesystemDenied, "path is outside the worker filesystem root")
	}
	body, err := os.ReadFile(evaluated)
	if err != nil {
		return nil, fail(CodeNotFound, "file not found")
	}
	return body, nil
}

func outside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
