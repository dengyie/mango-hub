package messageSender

import (
	"encoding/json"
	"fmt"

	"github.com/dengyie/mangohub/hub/utils/messageSender/factory"
)

func LoadProvider(name string, addition string) error {
	mu.Lock()
	defer mu.Unlock()
	constructor, exists := factory.GetConstructor(name)
	if !exists {
		return fmt.Errorf("message sender provider not found: %s", name)
	}

	provider := constructor()
	err := json.Unmarshal([]byte(addition), provider.GetConfiguration())
	if err != nil {
		return fmt.Errorf("failed to load config for provider %s: %w", name, err)
	}
	// Propagate Init failures instead of silently installing a broken
	// provider: a misconfigured JS sender used to pass the load step and then
	// fail at send time on every subsequent notification.
	if err := provider.Init(); err != nil {
		_ = provider.Destroy()
		return fmt.Errorf("failed to initialize provider %s: %w", name, err)
	}
	if currentProvider != nil {
		currentProvider.Destroy()
	}
	currentProvider = provider
	return nil
}
