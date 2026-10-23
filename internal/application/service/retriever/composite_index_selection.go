package retriever

import (
	"fmt"
	"slices"

	"github.com/Tencent/WeKnora/internal/types"
)

// SelectIndexTypes returns a new view over the authorized store binding. It
// never adds a backend or enables a retriever disabled on the knowledge base.
func (c *CompositeRetrieveEngine) SelectIndexTypes(selected []types.RetrieverType) (*CompositeRetrieveEngine, error) {
	for _, typ := range selected {
		if !c.SupportRetriever(typ) {
			return nil, fmt.Errorf("configured store does not support %s", typ)
		}
	}
	view := &CompositeRetrieveEngine{}
	for _, engine := range c.engineInfos {
		typesForEngine := make([]types.RetrieverType, 0, len(selected))
		for _, typ := range engine.retrieverType {
			if slices.Contains(selected, typ) {
				typesForEngine = append(typesForEngine, typ)
			}
		}
		if len(typesForEngine) > 0 {
			view.engineInfos = append(view.engineInfos, &engineInfo{retrieveEngine: engine.retrieveEngine, retrieverType: typesForEngine})
		}
	}
	if len(view.engineInfos) == 0 {
		return nil, fmt.Errorf("no index retriever enabled")
	}
	return view, nil
}
