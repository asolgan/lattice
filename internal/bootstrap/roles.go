package bootstrap

// RoleEntry describes a canonical role vertex and its aspects.
type RoleEntry struct {
	Key         string
	ID          string
	Description string
}

// CanonicalRoles returns the five canonical role vertices per Story 1.3 AC
// and the handoff brief decision #10.
// Only platformInternal gets permission grants in this story; the other four
// are seeded as vertices with description aspects only — their permission grants
// land in domain stories (3.6, 4.1, 5.1, etc.).
func CanonicalRoles() []RoleEntry {
	return []RoleEntry{
		{
			Key:         RoleConsumerKey,
			ID:          RoleConsumerID,
			Description: "A resident, tenant, or other end-consumer of platform services.",
		},
		{
			Key:         RoleFrontOfHouseKey,
			ID:          RoleFrontOfHouseID,
			Description: "Front-of-house staff with visibility into resident-facing operations.",
		},
		{
			Key:         RoleBackOfHouseKey,
			ID:          RoleBackOfHouseID,
			Description: "Back-of-house staff responsible for internal operational tasks.",
		},
		{
			Key:         RoleOperatorKey,
			ID:          RoleOperatorID,
			Description: "Platform operator with elevated management privileges.",
		},
		{
			Key:         RolePlatformIntlKey,
			ID:          RolePlatformIntlID,
			Description: "Platform-internal service actor role granting root-equivalent access to all operation types.",
		},
	}
}

// PlatformInternalPermissionData is the data payload for the permission vertex
// that grants scope=any to all operation types for the platformInternal role.
// Per Contract #7 §7.2 item 6 and handoff brief decision #10.
type PlatformInternalPermissionData struct {
	OperationScope string `json:"operationScope"`
	Note           string `json:"note"`
}

func PlatformInternalPermission() PlatformInternalPermissionData {
	return PlatformInternalPermissionData{
		OperationScope: "any",
		Note:           "Root-equivalent grant for internal service actors. Scope=any across all operation types.",
	}
}
