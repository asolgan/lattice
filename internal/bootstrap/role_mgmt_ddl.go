package bootstrap

// RoleMgmtDDLEntry describes one DDL meta-vertex for the role/permission domain.
type RoleMgmtDDLEntry struct {
	Key              string
	CanonicalName    string
	Class            string // meta.ddl.vertexType | meta.ddl.linkType
	Kind             string // "vertexType" | "linkType"
	PermittedCommands []string
	Description      string
	Script           string
}

// RoleMgmtDDLs returns the five DDL meta-vertex definitions for the
// role / permission / link domain (Story 3.6).
func RoleMgmtDDLs() []RoleMgmtDDLEntry {
	return []RoleMgmtDDLEntry{
		{
			Key:              DDLRoleKey,
			CanonicalName:    "role",
			Class:            "meta.ddl.vertexType",
			Kind:             "vertexType",
			PermittedCommands: []string{"CreateRole", "UpdateRole", "TombstoneRole"},
			Description:      "DDL for role vertices. Roles aggregate permissions and can be assigned to identities.",
			Script:           roleDDLScript,
		},
		{
			Key:              DDLPermissionKey,
			CanonicalName:    "permission",
			Class:            "meta.ddl.vertexType",
			Kind:             "vertexType",
			PermittedCommands: []string{"CreatePermission", "UpdatePermission", "TombstonePermission"},
			Description:      "DDL for permission vertices. Permissions represent the right to submit a specific operation type.",
			Script:           permissionDDLScript,
		},
		{
			Key:              DDLHoldsRoleKey,
			CanonicalName:    "holdsRole",
			Class:            "meta.ddl.linkType",
			Kind:             "linkType",
			PermittedCommands: []string{"AssignRole", "RevokeRole"},
			Description:      "DDL for holdsRole links. Connects an identity vertex to a role vertex, granting that role to the identity.",
			Script:           holdsRoleDDLScript,
		},
		{
			Key:              DDLGrantsPermissionKey,
			CanonicalName:    "grantsPermission",
			Class:            "meta.ddl.linkType",
			Kind:             "linkType",
			PermittedCommands: []string{"GrantPermission", "RevokePermission"},
			Description:      "DDL for grantsPermission links. Connects a permission vertex to a role vertex, granting that permission to all holders of the role.",
			Script:           grantsPermissionDDLScript,
		},
		{
			Key:              DDLReportsToKey,
			CanonicalName:    "reportsTo",
			Class:            "meta.ddl.linkType",
			Kind:             "linkType",
			PermittedCommands: []string{"AssignReportingChain", "RemoveReportingChain"},
			Description:      "DDL for reportsTo links. Connects one identity to a manager identity, enabling ephemeral task delegation traversal.",
			Script:           reportsToScript,
		},
	}
}

// RoleMgmtPermEntry describes one per-op permission vertex seeded at bootstrap.
type RoleMgmtPermEntry struct {
	Key           string
	ID            string
	OperationType string
	Scope         string
}

// RoleMgmtOperatorPermissions returns the 12 per-op permission vertices
// granted to the operator role (Story 3.6 Decision #2).
// The Capability Lens exact-operationType match requires one permission
// per concrete operation type.
func RoleMgmtOperatorPermissions() []RoleMgmtPermEntry {
	return []RoleMgmtPermEntry{
		{Key: PermCreateRoleKey, ID: PermCreateRoleID, OperationType: "CreateRole", Scope: "any"},
		{Key: PermUpdateRoleKey, ID: PermUpdateRoleID, OperationType: "UpdateRole", Scope: "any"},
		{Key: PermTombstoneRoleKey, ID: PermTombstoneRoleID, OperationType: "TombstoneRole", Scope: "any"},
		{Key: PermCreatePermissionKey, ID: PermCreatePermissionID, OperationType: "CreatePermission", Scope: "any"},
		{Key: PermUpdatePermissionKey, ID: PermUpdatePermissionID, OperationType: "UpdatePermission", Scope: "any"},
		{Key: PermTombstonePermissionKey, ID: PermTombstonePermissionID, OperationType: "TombstonePermission", Scope: "any"},
		{Key: PermAssignRoleKey, ID: PermAssignRoleID, OperationType: "AssignRole", Scope: "any"},
		{Key: PermRevokeRoleKey, ID: PermRevokeRoleID, OperationType: "RevokeRole", Scope: "any"},
		{Key: PermGrantPermissionKey, ID: PermGrantPermissionID, OperationType: "GrantPermission", Scope: "any"},
		{Key: PermRevokePermissionKey, ID: PermRevokePermissionID, OperationType: "RevokePermission", Scope: "any"},
		{Key: PermAssignReportingChainKey, ID: PermAssignReportingChainID, OperationType: "AssignReportingChain", Scope: "any"},
		{Key: PermRemoveReportingChainKey, ID: PermRemoveReportingChainID, OperationType: "RemoveReportingChain", Scope: "any"},
	}
}

// --- Starlark scripts ---

// roleDDLScript handles CreateRole / UpdateRole / TombstoneRole.
// Sandbox: no I/O, no time, no os; globals: state, op, ddl, nanoid.
// Returns: {"mutations": [...], "events": [...]} per Contract #3.
const roleDDLScript = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key, "document": {"class": cls, "isDeleted": False, "data": data}}

def make_aspect(key, cls, data):
    return {"op": "create", "key": key, "document": {"class": cls, "isDeleted": False, "data": data}}

def make_update(key, data):
    return {"op": "update", "key": key, "document": {"isDeleted": False, "data": data}}

def make_tombstone(key):
    return {"op": "tombstone", "key": key, "document": {"isDeleted": True, "data": {}}}

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "CreateRole":
        role_id = nanoid.new()
        role_key = "vtx.role." + role_id
        desc = p.description if hasattr(p, "description") else ""
        mutations = [
            make_vtx(role_key, "role", {"name": p.name}),
            make_aspect(role_key + ".description", "description", {"text": desc}),
        ]
        events = [{"class": "RoleCreated", "data": {"roleKey": role_key, "name": p.name}}]
        return {"mutations": mutations, "events": events}

    if ot == "UpdateRole":
        role_key = p.roleKey
        desc = p.description if hasattr(p, "description") else ""
        mutations = [make_update(role_key + ".description", {"text": desc})]
        events = [{"class": "RoleUpdated", "data": {"roleKey": role_key}}]
        return {"mutations": mutations, "events": events}

    if ot == "TombstoneRole":
        mutations = [make_tombstone(p.roleKey)]
        events = [{"class": "RoleTombstoned", "data": {"roleKey": p.roleKey}}]
        return {"mutations": mutations, "events": events}

    fail("role DDL: unknown operationType: " + ot)
`

// permissionDDLScript handles CreatePermission / UpdatePermission / TombstonePermission.
const permissionDDLScript = `
def make_vtx(key, cls, data):
    return {"op": "create", "key": key, "document": {"class": cls, "isDeleted": False, "data": data}}

def make_aspect(key, cls, data):
    return {"op": "create", "key": key, "document": {"class": cls, "isDeleted": False, "data": data}}

def make_update(key, data):
    return {"op": "update", "key": key, "document": {"isDeleted": False, "data": data}}

def make_tombstone(key):
    return {"op": "tombstone", "key": key, "document": {"isDeleted": True, "data": {}}}

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "CreatePermission":
        perm_id = nanoid.new()
        perm_key = "vtx.permission." + perm_id
        scope = p.scope if hasattr(p, "scope") else "any"
        note = p.note if hasattr(p, "note") else ""
        data = {"operationType": p.operationType, "scope": scope}
        if note != "":
            data["note"] = note
        mutations = [make_vtx(perm_key, "permission", data)]
        events = [{"class": "PermissionCreated", "data": {"permissionKey": perm_key, "operationType": p.operationType}}]
        return {"mutations": mutations, "events": events}

    if ot == "UpdatePermission":
        perm_key = p.permissionKey
        scope = p.scope if hasattr(p, "scope") else "any"
        mutations = [make_update(perm_key, {"operationType": p.operationType, "scope": scope})]
        events = [{"class": "PermissionUpdated", "data": {"permissionKey": perm_key}}]
        return {"mutations": mutations, "events": events}

    if ot == "TombstonePermission":
        mutations = [make_tombstone(p.permissionKey)]
        events = [{"class": "PermissionTombstoned", "data": {"permissionKey": p.permissionKey}}]
        return {"mutations": mutations, "events": events}

    fail("permission DDL: unknown operationType: " + ot)
`

// holdsRoleDDLScript handles AssignRole (create link) / RevokeRole (tombstone link).
// Link key: lnk.identity.<identityID>.holdsRole.role.<roleID>
// "identity" < "role" alphabetically → identity is younger (first segment).
const holdsRoleDDLScript = `
def make_link(key, younger, older, local_name):
    return {"op": "create", "key": key, "document": {
        "class": "holdsRole",
        "isDeleted": False,
        "youngerVertex": younger,
        "olderVertex": older,
        "localName": local_name,
        "data": {},
    }}

def make_tombstone(key):
    return {"op": "tombstone", "key": key, "document": {"isDeleted": True, "data": {}}}

def identity_id(key):
    parts = key.split(".")
    return parts[2] if len(parts) >= 3 else ""

def role_id(key):
    parts = key.split(".")
    return parts[2] if len(parts) >= 3 else ""

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "AssignRole":
        id_key = p.identityKey
        r_key = p.roleKey
        lnk_key = "lnk.identity." + identity_id(id_key) + ".holdsRole.role." + role_id(r_key)
        mutations = [make_link(lnk_key, id_key, r_key, "holdsRole")]
        events = [{"class": "RoleAssigned", "data": {"identityKey": id_key, "roleKey": r_key, "linkKey": lnk_key}}]
        return {"mutations": mutations, "events": events}

    if ot == "RevokeRole":
        id_key = p.identityKey
        r_key = p.roleKey
        lnk_key = "lnk.identity." + identity_id(id_key) + ".holdsRole.role." + role_id(r_key)
        mutations = [make_tombstone(lnk_key)]
        events = [{"class": "RoleRevoked", "data": {"identityKey": id_key, "roleKey": r_key}}]
        return {"mutations": mutations, "events": events}

    fail("holdsRole DDL: unknown operationType: " + ot)
`

// grantsPermissionDDLScript handles GrantPermission / RevokePermission.
// Link key: lnk.permission.<permID>.grantsPermission.role.<roleID>
// "permission" < "role" alphabetically → permission is younger (first segment).
const grantsPermissionDDLScript = `
def make_link(key, younger, older, local_name):
    return {"op": "create", "key": key, "document": {
        "class": "grantsPermission",
        "isDeleted": False,
        "youngerVertex": younger,
        "olderVertex": older,
        "localName": local_name,
        "data": {},
    }}

def make_tombstone(key):
    return {"op": "tombstone", "key": key, "document": {"isDeleted": True, "data": {}}}

def perm_id(key):
    parts = key.split(".")
    return parts[2] if len(parts) >= 3 else ""

def role_id(key):
    parts = key.split(".")
    return parts[2] if len(parts) >= 3 else ""

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "GrantPermission":
        p_key = p.permissionKey
        r_key = p.roleKey
        lnk_key = "lnk.permission." + perm_id(p_key) + ".grantsPermission.role." + role_id(r_key)
        mutations = [make_link(lnk_key, p_key, r_key, "grantsPermission")]
        events = [{"class": "PermissionGranted", "data": {"permissionKey": p_key, "roleKey": r_key, "linkKey": lnk_key}}]
        return {"mutations": mutations, "events": events}

    if ot == "RevokePermission":
        p_key = p.permissionKey
        r_key = p.roleKey
        lnk_key = "lnk.permission." + perm_id(p_key) + ".grantsPermission.role." + role_id(r_key)
        mutations = [make_tombstone(lnk_key)]
        events = [{"class": "PermissionRevoked", "data": {"permissionKey": p_key, "roleKey": r_key}}]
        return {"mutations": mutations, "events": events}

    fail("grantsPermission DDL: unknown operationType: " + ot)
`

// reportsToScript handles AssignReportingChain / RemoveReportingChain.
// Both vertices are identities. Link key uses NanoID order (alphabetical by ID).
const reportsToScript = `
def make_link(key, younger, older, local_name):
    return {"op": "create", "key": key, "document": {
        "class": "reportsTo",
        "isDeleted": False,
        "youngerVertex": younger,
        "olderVertex": older,
        "localName": local_name,
        "data": {},
    }}

def make_tombstone(key):
    return {"op": "tombstone", "key": key, "document": {"isDeleted": True, "data": {}}}

def identity_id(key):
    parts = key.split(".")
    return parts[2] if len(parts) >= 3 else ""

def link_key(report_key, manager_key):
    r_id = identity_id(report_key)
    m_id = identity_id(manager_key)
    if r_id < m_id:
        return "lnk.identity." + r_id + ".reportsTo.identity." + m_id
    return "lnk.identity." + m_id + ".reportsTo.identity." + r_id

def execute(state, op):
    ot = op.operationType
    p = op.payload

    if ot == "AssignReportingChain":
        r_key = p.reportKey
        m_key = p.managerKey
        lnk = link_key(r_key, m_key)
        mutations = [make_link(lnk, r_key, m_key, "reportsTo")]
        events = [{"class": "ReportingChainAssigned", "data": {"reportKey": r_key, "managerKey": m_key, "linkKey": lnk}}]
        return {"mutations": mutations, "events": events}

    if ot == "RemoveReportingChain":
        r_key = p.reportKey
        m_key = p.managerKey
        lnk = link_key(r_key, m_key)
        mutations = [make_tombstone(lnk)]
        events = [{"class": "ReportingChainRemoved", "data": {"reportKey": r_key, "managerKey": m_key}}]
        return {"mutations": mutations, "events": events}

    fail("reportsTo DDL: unknown operationType: " + ot)
`
