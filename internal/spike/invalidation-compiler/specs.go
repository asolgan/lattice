// Package invalidationcompiler is a NON-SHIPPING throwaway spike (Story 12.2).
//
// It proves a narrow invalidation compiler can derive the affected-anchor set
// from the real full-engine openCypher AST of the two actor-aggregate lenses
// (capabilityEphemeral, myTasks), and that the derived set is a sound subset of
// the broad ActorEnumerator BFS. It imports production packages read-only and
// modifies none of them. Nothing here is wired into the live projection path.
//
// 12.3 builds the production compiler; it does NOT ship this package.
package invalidationcompiler

// myTasksSpec and capabilityEphemeralSpec are pinned VERBATIM snapshots of the
// real lens cypher. They are the compiler inputs the spike proves against.
//
// source: packages/orchestration-base/lenses.go (capabilityEphemeralSpec /
// myTasksSpec, snapshot 2026-06-14)

const myTasksSpec = `
MATCH (identity:identity {key: $actorKey})

OPTIONAL MATCH (identity)<-[:assignedTo]-(task:task)
  WHERE task.data.status = 'open'
OPTIONAL MATCH (task)-[:forOperation]->(op)
OPTIONAL MATCH (task)-[:scopedTo]->(tgt)

RETURN
  identity.key AS actorKey,
  collect(DISTINCT {
    taskKey: task.key,
    assignee: identity.key,
    forOperation: op.key,
    scopedTo: tgt.key,
    expiresAt: task.data.expiresAt
  }) AS openTasks
`

const capabilityEphemeralSpec = `
MATCH (identity:identity {key: $actorKey})

// --- direct assignments ---
OPTIONAL MATCH (identity)<-[:assignedTo]-(task:task)
  WHERE task.data.expiresAt > $now
OPTIONAL MATCH (task)-[:forOperation]->(op)
OPTIONAL MATCH (task)-[:scopedTo]->(tgt)

// --- manager delegation: reportsTo 2-hop ---
// identity is the manager; each report reportsTo identity, so identity
// inherits the tasks assigned to its reports (downward delegation).
OPTIONAL MATCH (identity)<-[:reportsTo]-(report:identity)<-[:assignedTo]-(task2:task)
  WHERE task2.data.expiresAt > $now
OPTIONAL MATCH (task2)-[:forOperation]->(op2)
OPTIONAL MATCH (task2)-[:scopedTo]->(tgt2)

RETURN
  identity.key AS actorKey,
  collect(DISTINCT {
    source: "task",
    taskKey: task.key,
    operationType: op.data.operationType,
    target: tgt.key,
    expiresAt: task.data.expiresAt
  }) + collect(DISTINCT {
    source: "task",
    taskKey: task2.key,
    operationType: op2.data.operationType,
    target: tgt2.key,
    expiresAt: task2.data.expiresAt
  }) AS ephemeralGrants
`
