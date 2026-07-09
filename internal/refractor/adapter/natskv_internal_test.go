package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStoredProjectionSeq(t *testing.T) {
	t.Run("empty data", func(t *testing.T) {
		seq, ok := storedProjectionSeq(nil)
		assert.False(t, ok)
		assert.Zero(t, seq)
	})
	t.Run("malformed JSON", func(t *testing.T) {
		seq, ok := storedProjectionSeq([]byte(`{not json`))
		assert.False(t, ok)
		assert.Zero(t, seq)
	})
	t.Run("legacy doc with no projectionSeq field", func(t *testing.T) {
		seq, ok := storedProjectionSeq([]byte(`{"row":{"a":1}}`))
		assert.False(t, ok)
		assert.Zero(t, seq)
	})
	t.Run("valid watermark", func(t *testing.T) {
		seq, ok := storedProjectionSeq([]byte(`{"projectionSeq":42}`))
		assert.True(t, ok)
		assert.Equal(t, uint64(42), seq)
	})
	t.Run("zero watermark", func(t *testing.T) {
		seq, ok := storedProjectionSeq([]byte(`{"projectionSeq":0}`))
		assert.True(t, ok)
		assert.Zero(t, seq)
	})
	t.Run("negative watermark treated as malformed, not wrapped", func(t *testing.T) {
		// A hand-corrupted or pre-guard doc could carry a negative value; the
		// float64->uint64 conversion would otherwise wrap to a bogus near-max
		// value that permanently no-ops every future write to the key.
		seq, ok := storedProjectionSeq([]byte(`{"projectionSeq":-1}`))
		assert.False(t, ok)
		assert.Zero(t, seq)
	})
	t.Run("non-numeric watermark", func(t *testing.T) {
		seq, ok := storedProjectionSeq([]byte(`{"projectionSeq":"not-a-number"}`))
		assert.False(t, ok)
		assert.Zero(t, seq)
	})
	t.Run("null watermark", func(t *testing.T) {
		seq, ok := storedProjectionSeq([]byte(`{"projectionSeq":null}`))
		assert.False(t, ok)
		assert.Zero(t, seq)
	})
}
