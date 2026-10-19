package applecontainer

import (
	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/plan"
)

type dialect struct{}

var _ engine.Engine = dialect{}

func New() engine.Engine { return dialect{} }

func (dialect) Name() string                    { return "container" }
func (dialect) Binary() string                  { return "container" }
func (dialect) Capabilities() plan.Capabilities { return plan.Capabilities{Healthcheck: false} }
