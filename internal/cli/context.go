package cli

import "aivm/internal/lifecycle"

// App is the central dependency container for the aivm CLI.
// All orchestration and infrastructure access goes through Lifecycle.
type App struct {
	Lifecycle *lifecycle.LifecycleService
}
