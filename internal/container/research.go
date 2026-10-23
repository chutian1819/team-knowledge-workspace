package container

import (
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/handler"
	"go.uber.org/dig"
)

// registerResearch composes the existing auth, knowledge, wiki and task
// facilities. It registers no extra stores, retrievers or model providers.
func registerResearch(container *dig.Container) {
	must(container.Provide(repository.NewResearchCaptureRepository))
	must(container.Provide(service.NewResearchCaptureService))
	must(container.Provide(handler.NewResearchCaptureHandler))
	must(container.Provide(service.NewResearchDraftService))
	must(container.Provide(handler.NewResearchDraftHandler))
}
