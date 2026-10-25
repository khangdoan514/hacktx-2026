package platform

import "os"

// Headers the application API sets after it has checked the employee.
// The private agent process trusts them only together with InternalHeader.
const (
	InternalHeader = "X-NMC3-Internal"
	ActorHeader    = "X-NMC3-Actor"
	UserHeader     = "X-NMC3-User"
	EmailHeader    = "X-NMC3-Email"
	TenantHeader   = "X-NMC3-Tenant"
	RolesHeader    = "X-NMC3-Roles"
)

// InternalToken is the shared secret between cmd/api and the agent process.
// An empty value means the agent process does not require the header.
func InternalToken() string {
	return os.Getenv("NMC3_INTERNAL_TOKEN")
}
