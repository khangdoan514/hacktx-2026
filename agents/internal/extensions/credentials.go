package extensions

import (
	"strings"
	"sync"
)

// MemoryCredentials keeps secret values in process memory, keyed by tenant
// and reference. Resolve never returns another tenant's value.
type MemoryCredentials struct {
	mu    sync.Mutex
	items map[string]Credential
}

func NewMemoryCredentials() *MemoryCredentials {
	return &MemoryCredentials{items: map[string]Credential{}}
}

func (m *MemoryCredentials) Put(cred Credential) error {
	if m == nil {
		return fail(CodeInvalidInput, "credential store is not available")
	}
	if !validToken(cred.TenantID) || !validToken(cred.Ref) {
		return fail(CodeInvalidInput, "credential reference is invalid")
	}
	if len(cred.Value) < 8 || len(cred.Value) > 4096 || strings.ContainsAny(cred.Value, "\r\n") {
		return fail(CodeInvalidInput, "credential value is not allowed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[cred.TenantID+"\x00"+cred.Ref] = cred
	return nil
}

func (m *MemoryCredentials) Resolve(tenantID, ref string) (Credential, bool) {
	if m == nil || !validToken(tenantID) || !validToken(ref) {
		return Credential{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cred, ok := m.items[tenantID+"\x00"+ref]
	if !ok {
		return Credential{}, false
	}
	return cred, true
}
