package loom

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestInferExternalTaskReads(t *testing.T) {
	const subj = "vtx.identity.BBsubjectHJKMNPQRS"

	cases := []struct {
		name   string
		params string // raw JSON (empty string ⇒ nil params)
		want   []string
		errSub string // non-empty ⇒ expect an error containing this substring
	}{
		{
			name:   "nil params reads only the subject root",
			params: "",
			want:   []string{subj},
		},
		{
			name:   "empty object reads only the subject root",
			params: `{}`,
			want:   []string{subj},
		},
		{
			name:   "only literal params reads only the subject root",
			params: `{"family":"backgroundCheck","retries":3,"async":true}`,
			want:   []string{subj},
		},
		{
			name:   "subject.data.<field> reads the subject root (no extra aspect)",
			params: `{"name":"subject.data.fullName"}`,
			want:   []string{subj},
		},
		{
			name:   "subject.<aspect>.data.<field> adds the aspect key",
			params: `{"fullName":"subject.demographics.data.fullName"}`,
			want:   []string{subj, subj + ".demographics"},
		},
		{
			name:   "mixed literal + subject token",
			params: `{"family":"backgroundCheck","fullName":"subject.demographics.data.fullName"}`,
			want:   []string{subj, subj + ".demographics"},
		},
		{
			name:   "multiple distinct aspects sorted deterministically",
			params: `{"z":"subject.zeta.data.f","a":"subject.alpha.data.g","m":"subject.demographics.data.fullName"}`,
			// param-key order is a,m,z → alpha, demographics, zeta
			want: []string{subj, subj + ".alpha", subj + ".demographics", subj + ".zeta"},
		},
		{
			name:   "duplicate aspect across params is deduped",
			params: `{"fullName":"subject.demographics.data.fullName","dob":"subject.demographics.data.dob"}`,
			want:   []string{subj, subj + ".demographics"},
		},
		{
			name:   "non-string values are literals (ignored)",
			params: `{"count":7,"flag":false,"nested":{"k":"subject.x.data.y"}}`,
			want:   []string{subj},
		},
		{
			name:   "params that is not a JSON object passes through (no inference)",
			params: `["subject.demographics.data.fullName"]`,
			want:   []string{subj},
		},
		{
			name:   "malformed subject token fails loud",
			params: `{"bad":"subject.demographics.fullName"}`,
			errSub: "malformed subject template",
		},
		{
			name:   "subject token with too many segments fails loud",
			params: `{"bad":"subject.a.b.data.c"}`,
			errSub: "malformed subject template",
		},
		{
			name:   "bare subject. token fails loud",
			params: `{"bad":"subject."}`,
			errSub: "malformed subject template",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.params != "" {
				raw = json.RawMessage(tc.params)
			}
			got, err := inferExternalTaskReads(subj, raw)
			if tc.errSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (reads=%v)", tc.errSub, got)
				}
				if !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("reads mismatch:\n got %v\nwant %v", got, tc.want)
			}
		})
	}
}
