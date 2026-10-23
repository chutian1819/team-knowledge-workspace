package service

import (
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestBuiltinTeamModelsOnlyUseActiveAdministratorDefaults(t *testing.T) {
	require.NoError(t, types.LoadBuiltinAgentsConfig(filepath.Join("..", "..", "..", "config")))
	models := []*types.Model{
		nil,
		{ID: "private", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive, IsDefault: true},
		{ID: "disabled", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusDownloadFailed, IsDefault: true, IsBuiltin: true},
		{ID: "team", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive, IsDefault: true, IsBuiltin: true, Parameters: types.ModelParameters{SupportsVision: true}},
		{ID: "rerank", Type: types.ModelTypeRerank, Status: types.ModelStatusActive, IsDefault: true, IsBuiltin: true},
		{ID: "vision", Type: types.ModelTypeVLLM, Status: types.ModelStatusActive, IsDefault: true, IsBuiltin: true},
	}
	for _, id := range []string{types.BuiltinQuickAnswerID, types.BuiltinSmartReasoningID} {
		a := &types.CustomAgent{ID: id}
		applyBuiltinTeamModels(a, models, true)
		require.Equal(t, "team", a.Config.ModelID)
		require.Equal(t, "rerank", a.Config.RerankModelID)
		require.Equal(t, "vision", a.Config.VLMModelID)
		require.True(t, a.Config.ImageUploadEnabled)
	}
	custom := &types.CustomAgent{ID: "custom"}
	applyBuiltinTeamModels(custom, models, true)
	require.Empty(t, custom.Config.ModelID)
	require.False(t, custom.Config.ImageUploadEnabled)
	saved := &types.CustomAgent{ID: types.BuiltinQuickAnswerID, Config: types.CustomAgentConfig{ModelID: "explicit", RerankModelID: "explicit-rerank"}}
	applyBuiltinTeamModels(saved, models, false)
	require.Equal(t, "explicit", saved.Config.ModelID)
	require.Equal(t, "explicit-rerank", saved.Config.RerankModelID)
	require.False(t, saved.Config.ImageUploadEnabled)
	require.Empty(t, saved.Config.VLMModelID)
	// Missing prerequisites stay visible; no arbitrary/private model fallback.
	empty := &types.CustomAgent{ID: types.BuiltinQuickAnswerID}
	applyBuiltinTeamModels(empty, models[:3], true)
	require.Empty(t, empty.Config.ModelID)
	require.False(t, empty.Config.ImageUploadEnabled)
}
