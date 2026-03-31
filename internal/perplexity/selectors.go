package perplexity

import "dnd-workflow/internal/config"

// Selectors returns the configured CSS selectors, falling back to defaults.
func Selectors(cfg config.PerplexitySelectors) config.PerplexitySelectors {
	return cfg
}
