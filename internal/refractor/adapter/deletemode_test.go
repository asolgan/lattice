package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDeleteMode(t *testing.T) {
	t.Run("empty defaults hard", func(t *testing.T) {
		m, err := ParseDeleteMode("")
		require.NoError(t, err)
		assert.Equal(t, DeleteModeHard, m)
	})
	t.Run("hard", func(t *testing.T) {
		m, err := ParseDeleteMode("hard")
		require.NoError(t, err)
		assert.Equal(t, DeleteModeHard, m)
	})
	t.Run("soft", func(t *testing.T) {
		m, err := ParseDeleteMode("soft")
		require.NoError(t, err)
		assert.Equal(t, DeleteModeSoft, m)
	})
	t.Run("invalid", func(t *testing.T) {
		_, err := ParseDeleteMode("bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid delete mode")
	})
}
