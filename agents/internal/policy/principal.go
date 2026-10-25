package policy

import "github.com/khangdoan514/hacktx-2026/internal/platform"

// PrincipalFromActor copies an employee actor loaded by organization.Load.
// TenantID is the NMC2 organization id. An actor without a user id or
// organization is rejected so anonymous local mode cannot start a run.
func PrincipalFromActor(actor platform.Actor) (Principal, error) {
	if actor.ID == "" || actor.OrgID == "" {
		return Principal{}, errCode(CodeDenied, "authenticated employee identity is required")
	}
	return Principal{
		UserID:   actor.ID,
		Email:    actor.Email,
		TenantID: actor.OrgID,
		Roles:    cloneStrings(actor.Roles),
		Kind:     KindUser,
	}, nil
}

// ServicePrincipal is an already-authenticated non-employee caller in one tenant.
// This package does not verify service credentials. Services cannot approve a
// protected write unless the pinned policy sets AllowServiceApproval.
func ServicePrincipal(tenantID, serviceID string, roles []string) (Principal, error) {
	if !validID(tenantID) || !validID(serviceID) {
		return Principal{}, errCode(CodeDenied, "authenticated service identity is required")
	}
	return Principal{
		UserID:   serviceID,
		TenantID: tenantID,
		Roles:    cloneStrings(roles),
		Kind:     KindService,
	}, nil
}

// Has reports a role using platform.Actor so agent policy and the NMC2
// directory share one role check.
func (p Principal) Has(role string) bool {
	return (platform.Actor{Roles: p.Roles}).Has(role)
}

func (p Principal) HasAny(roles ...string) bool {
	for _, role := range roles {
		if p.Has(role) {
			return true
		}
	}
	return false
}

func (p Principal) validate() error {
	if p.UserID == "" || p.TenantID == "" {
		return errCode(CodeDenied, "authenticated tenant identity is required")
	}
	if !validID(p.UserID) || !validID(p.TenantID) {
		return errCode(CodeInvalidInput, "principal identity is invalid")
	}
	if p.Kind != KindUser && p.Kind != KindService {
		return errCode(CodeInvalidInput, "principal kind must be user or service")
	}
	return nil
}

func (p Principal) canStart() bool {
	return p.HasAny(RoleAIEngineer, RoleOperator, RoleAdministrator)
}

func (p Principal) canInspect(run RunRecord) bool {
	if p.TenantID == run.TenantID && p.UserID == run.RequesterID {
		return true
	}
	return p.HasAny(RoleAuditor, RoleAdministrator, RoleOperator, RoleAIEngineer, RoleSecurityReviewer)
}

func (p Principal) canApprove(policy Policy) bool {
	if p.Kind == KindService && !policy.AllowServiceApproval {
		return false
	}
	return p.HasAny(approverRoles(policy)...)
}

func (p Principal) canReconcile() bool {
	return p.Kind == KindUser && p.HasAny(RoleOperator, RoleAdministrator)
}

func approverRoles(policy Policy) []string {
	if len(policy.ApproverRoles) == 0 {
		return []string{RoleReleaseApprover, RoleSecurityReviewer, RoleAdministrator}
	}
	return policy.ApproverRoles
}
