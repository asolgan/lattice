package main

import "testing"

// TestComputeDocuments_ScopesToOwnerAndReshapes proves the Documents assembler
// keeps only objectAttachments-prefixed rows, scopes them to the applicant's
// owner key, drops the degenerate {ownerKey:null} artifact, and reshapes each
// surviving row to its oid + display metadata.
func TestComputeDocuments_ScopesToOwnerAndReshapes(t *testing.T) {
	const leaseapp = "vtx.leaseapp.app1"
	entries := map[string]string{
		// mine — a pdf attached to my leaseapp
		"objectAttachments.aaa": `{"entityKey":"vtx.object.aaa","storeName":"s1","contentType":"application/pdf","size":4096,"owners":[{"ownerKey":"vtx.leaseapp.app1"}]}`,
		// someone else's — different owner, must be excluded when scoped
		"objectAttachments.bbb": `{"entityKey":"vtx.object.bbb","storeName":"s2","contentType":"image/png","size":100,"owners":[{"ownerKey":"vtx.leaseapp.other"}]}`,
		// fully detached — only the null artifact, must be excluded
		"objectAttachments.ccc": `{"entityKey":"vtx.object.ccc","storeName":"s3","contentType":"image/png","size":50,"owners":[{"ownerKey":null}]}`,
		// not a lens row — a leaseApplicationComplete projection sharing the bucket
		"leaseApplicationComplete.zzz": `{"entityKey":"vtx.leaseapp.app1"}`,
		// undecodable — skipped, never panics
		"objectAttachments.ddd": `{`,
	}

	docs := computeDocuments(keysOf(entries), fakeKV(entries), []string{leaseapp})
	if len(docs) != 1 {
		t.Fatalf("want exactly my 1 document, got %d: %+v", len(docs), docs)
	}
	d := docs[0]
	if d.OID != "aaa" {
		t.Errorf("oid: want aaa (stripped of the vtx.object. prefix), got %q", d.OID)
	}
	if d.OwnerKey != leaseapp {
		t.Errorf("ownerKey: want %q, got %q", leaseapp, d.OwnerKey)
	}
	if d.ContentType != "application/pdf" || d.Size != 4096 {
		t.Errorf("metadata mismatch: %+v", d)
	}
}

// TestComputeDocuments_UnscopedListsAll proves an empty applicant lists every
// document that has at least one real owner (the operator-style view).
func TestComputeDocuments_UnscopedListsAll(t *testing.T) {
	entries := map[string]string{
		"objectAttachments.aaa": `{"entityKey":"vtx.object.aaa","storeName":"s1","contentType":"application/pdf","size":1,"owners":[{"ownerKey":"vtx.leaseapp.app1"}]}`,
		"objectAttachments.bbb": `{"entityKey":"vtx.object.bbb","storeName":"s2","contentType":"image/png","size":2,"owners":[{"ownerKey":"vtx.identity.id2"}]}`,
		// no real owner — excluded even when unscoped
		"objectAttachments.ccc": `{"entityKey":"vtx.object.ccc","storeName":"s3","owners":[{"ownerKey":null}]}`,
	}
	docs := computeDocuments(keysOf(entries), fakeKV(entries), nil)
	if len(docs) != 2 {
		t.Fatalf("want 2 owned documents, got %d: %+v", len(docs), docs)
	}
	// sorted by oid for a stable view
	if docs[0].OID != "aaa" || docs[1].OID != "bbb" {
		t.Errorf("want oid-sorted [aaa bbb], got [%s %s]", docs[0].OID, docs[1].OID)
	}
}

// TestComputeDocuments_UnionsMultipleOwners proves the "all my documents" view:
// a set of owner keys (the applicant's identity + each application) unions every
// document linked to any of them, while documents owned only by an out-of-scope
// key stay excluded — and each surviving row reports the in-scope owner it matched.
func TestComputeDocuments_UnionsMultipleOwners(t *testing.T) {
	const identity = "vtx.identity.me"
	const app1 = "vtx.leaseapp.app1"
	const app2 = "vtx.leaseapp.app2"
	entries := map[string]string{
		// mine — one on my identity, one on each of my two applications
		"objectAttachments.aaa": `{"entityKey":"vtx.object.aaa","storeName":"s1","contentType":"image/png","size":1,"owners":[{"ownerKey":"vtx.identity.me"}]}`,
		"objectAttachments.bbb": `{"entityKey":"vtx.object.bbb","storeName":"s2","contentType":"application/pdf","size":2,"owners":[{"ownerKey":"vtx.leaseapp.app1"}]}`,
		"objectAttachments.ccc": `{"entityKey":"vtx.object.ccc","storeName":"s3","contentType":"application/pdf","size":3,"owners":[{"ownerKey":"vtx.leaseapp.app2"}]}`,
		// someone else's application — excluded from my union
		"objectAttachments.ddd": `{"entityKey":"vtx.object.ddd","storeName":"s4","contentType":"image/png","size":4,"owners":[{"ownerKey":"vtx.leaseapp.other"}]}`,
	}

	docs := computeDocuments(keysOf(entries), fakeKV(entries), []string{identity, app1, app2})
	if len(docs) != 3 {
		t.Fatalf("want my 3 documents unioned across identity + 2 apps, got %d: %+v", len(docs), docs)
	}
	want := map[string]string{"aaa": identity, "bbb": app1, "ccc": app2}
	for _, d := range docs {
		if want[d.OID] != d.OwnerKey {
			t.Errorf("doc %s: want owner %q, got %q", d.OID, want[d.OID], d.OwnerKey)
		}
	}
}
