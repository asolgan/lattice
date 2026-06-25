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

	docs := computeDocuments(keysOf(entries), fakeKV(entries), leaseapp)
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
	docs := computeDocuments(keysOf(entries), fakeKV(entries), "")
	if len(docs) != 2 {
		t.Fatalf("want 2 owned documents, got %d: %+v", len(docs), docs)
	}
	// sorted by oid for a stable view
	if docs[0].OID != "aaa" || docs[1].OID != "bbb" {
		t.Errorf("want oid-sorted [aaa bbb], got [%s %s]", docs[0].OID, docs[1].OID)
	}
}
