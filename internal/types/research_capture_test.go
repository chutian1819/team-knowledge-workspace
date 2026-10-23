package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResearchSourceVersionDoesNotExposeOriginalStorageLocator(t *testing.T) {
	for _, path := range []string{"", "resource://private-historical-original"} {
		encoded, err := json.Marshal(ResearchSourceVersion{ID: "version", OriginalPath: path, FileName: "report.docx"})
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(encoded, &fields))
		require.NotContains(t, fields, "original_path")
		require.Equal(t, path != "", fields["has_original"])
		require.Equal(t, "report.docx", fields["file_name"])
		if path != "" {
			require.NotContains(t, string(encoded), path)
		}
	}
}
