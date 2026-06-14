package simple

import "testing"

func TestAnalyzeInvalidationCoverage_LiveSpecsCovered(t *testing.T) {
	for _, body := range []string{myTasksSpec, capabilityEphemeralSpec} {
		res, err := AnalyzeInvalidationCoverage(body)
		if err != nil {
			t.Fatalf("coverage err: %v", err)
		}
		if !res.Covered {
			t.Fatalf("expected live spec covered, got uncovered: %s", res.Reason)
		}
	}
}

func TestAnalyzeInvalidationCoverage_UndirectedHopNotCovered(t *testing.T) {
	body := `
MATCH (identity:identity {key: $actorKey})
OPTIONAL MATCH (identity)-[:assignedTo]-(task:task)
RETURN identity.key AS actorKey, collect(task.key) AS tasks
`
	res, err := AnalyzeInvalidationCoverage(body)
	if err != nil {
		t.Fatalf("coverage err: %v", err)
	}
	if res.Covered {
		t.Fatalf("expected undirected hop to be NOT covered")
	}
}

func TestAnalyzeInvalidationCoverage_PatternPredicateWhereNotCovered(t *testing.T) {
	// A WHERE asserting existence of ANOTHER path can broaden the matched set.
	body := `
MATCH (identity:identity {key: $actorKey})
OPTIONAL MATCH (identity)<-[:assignedTo]-(task:task)
  WHERE NOT (task)-[:blockedBy]->(:blocker)
RETURN identity.key AS actorKey, collect(task.key) AS tasks
`
	res, err := AnalyzeInvalidationCoverage(body)
	if err != nil {
		t.Fatalf("coverage err: %v", err)
	}
	if res.Covered {
		t.Fatalf("expected pattern-predicate WHERE to be NOT covered")
	}
}

func TestAnalyzeInvalidationCoverage_NarrowingWhereCovered(t *testing.T) {
	// A plain scalar WHERE only narrows — covered (the compiler ignores it).
	body := `
MATCH (identity:identity {key: $actorKey})
OPTIONAL MATCH (identity)<-[:assignedTo]-(task:task)
  WHERE task.data.status = 'open' AND task.data.priority > 3
RETURN identity.key AS actorKey, collect(task.key) AS tasks
`
	res, err := AnalyzeInvalidationCoverage(body)
	if err != nil {
		t.Fatalf("coverage err: %v", err)
	}
	if !res.Covered {
		t.Fatalf("expected narrowing WHERE to be covered, got: %s", res.Reason)
	}
}

func TestAnalyzeInvalidationCoverage_DisconnectedPatternNotCovered(t *testing.T) {
	// The second MATCH starts from `other`, a variable never reached from the
	// anchor — its leaf changes would not invalidate the anchor. Fail closed.
	body := `
MATCH (identity:identity {key: $actorKey})
OPTIONAL MATCH (identity)<-[:assignedTo]-(task:task)
OPTIONAL MATCH (other:identity)<-[:assignedTo]-(orphan:task)
RETURN identity.key AS actorKey, collect(task.key) AS tasks
`
	res, err := AnalyzeInvalidationCoverage(body)
	if err != nil {
		t.Fatalf("coverage err: %v", err)
	}
	if res.Covered {
		t.Fatalf("expected disconnected (non-anchor-rooted) pattern to be NOT covered")
	}
}

func TestAnalyzeInvalidationCoverage_VariableLengthHopNotCovered(t *testing.T) {
	// A variable-length hop is not a single fixed hop; the reverse walk cannot
	// explore its depths. Fail closed.
	body := `
MATCH (identity:identity {key: $actorKey})
OPTIONAL MATCH (identity)<-[:reportsTo*1..3]-(report:identity)
RETURN identity.key AS actorKey, collect(report.key) AS reports
`
	res, err := AnalyzeInvalidationCoverage(body)
	if err != nil {
		t.Fatalf("coverage err: %v", err)
	}
	if res.Covered {
		t.Fatalf("expected variable-length hop to be NOT covered")
	}
}

func TestAnalyzeInvalidationCoverage_ReturnPatternComprehensionNotCovered(t *testing.T) {
	// A RETURN pattern comprehension reads a path that appears in no MATCH; the
	// anchor-rooted reverse walk cannot see a change to that path. Fail closed.
	body := `
MATCH (identity:identity {key: $actorKey})
RETURN identity.key AS actorKey,
  [(identity)<-[:assignedTo]-(task:task) | task.key] AS tasks
`
	res, err := AnalyzeInvalidationCoverage(body)
	if err != nil {
		t.Fatalf("coverage err: %v", err)
	}
	if res.Covered {
		t.Fatalf("expected RETURN pattern comprehension to be NOT covered")
	}
}

func TestAnalyzeInvalidationCoverage_AnchorOnlyCovered(t *testing.T) {
	// An anchor-only lens has no traversal steps: only the anchor's own vertex
	// change matters, handled by Execution. It is sound → covered (must NOT fail
	// closed for the absence of a reverse-walk branch).
	body := `
MATCH (identity:identity {key: $actorKey})
RETURN identity.key AS actorKey, identity.data.name AS name
`
	res, err := AnalyzeInvalidationCoverage(body)
	if err != nil {
		t.Fatalf("coverage err: %v", err)
	}
	if !res.Covered {
		t.Fatalf("expected anchor-only lens to be covered, got: %s", res.Reason)
	}
}
