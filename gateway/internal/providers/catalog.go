// Package providers is the catalog of model providers the owner can choose from.
//
// The gateway resolves the owner's choice into a small set of environment
// variables for the agent worker, so provider details (base URLs, which secret
// holds the key) live in exactly one place.
package providers

// Kinds of provider adapters implemented by the agent worker.
const (
	KindAnthropic        = "anthropic"         // Claude API via the official Anthropic SDK
	KindOpenAICompatible = "openai_compatible" // OpenAI and compatible APIs via the official OpenAI SDK
	KindClaudeCode       = "claude_code"       // the owner's local Claude Code CLI and its login
	KindDemo             = "demo"              // offline sample answers
)

// Auto picks Claude when an Anthropic key is stored, otherwise the demo provider.
const Auto = "auto"

type Provider struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Description     string   `json:"description"`
	SecretName      string   `json:"secret_name,omitempty"`
	KeyRequired     bool     `json:"key_required"`
	BaseURL         string   `json:"base_url,omitempty"`
	BaseURLEditable bool     `json:"base_url_editable"`
	DefaultModel    string   `json:"default_model"`
	SuggestedModels []string `json:"suggested_models"`
	WebSearch       bool     `json:"web_search"`
}

var catalog = []Provider{
	{
		ID: "anthropic", Name: "Anthropic Claude API", Kind: KindAnthropic,
		Description: "Claude models with your Anthropic API key. Includes built-in web search.",
		SecretName:  "ANTHROPIC_API_KEY", KeyRequired: true,
		DefaultModel:    "claude-opus-5-5",
		SuggestedModels: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"},
		WebSearch:       true,
	},
	{
		ID: "claude_code", Name: "Claude Code (your subscription)", Kind: KindClaudeCode,
		Description:     "Uses the Claude Code CLI installed and signed in on this PC, so work runs on your existing Claude plan. Cubicle never sees your login.",
		DefaultModel:    "sonnet",
		SuggestedModels: []string{"opus", "sonnet", "haiku"},
		WebSearch:       true,
	},
	{
		ID: "openai", Name: "OpenAI", Kind: KindOpenAICompatible,
		Description: "GPT models with your OpenAI API key.",
		SecretName:  "OPENAI_API_KEY", KeyRequired: true,
		BaseURL:         "https://api.openai.com/v1",
		DefaultModel:    "gpt-5",
		SuggestedModels: []string{"gpt-5", "gpt-5-mini"},
	},
	{
		ID: "gemini", Name: "Google Gemini", Kind: KindOpenAICompatible,
		Description: "Gemini models with a Google AI Studio API key.",
		SecretName:  "GEMINI_API_KEY", KeyRequired: true,
		BaseURL:         "https://generativelanguage.googleapis.com/v1beta/openai/",
		DefaultModel:    "gemini-2.5-pro",
		SuggestedModels: []string{"gemini-2.5-pro", "gemini-2.5-flash"},
	},
	{
		ID: "openrouter", Name: "OpenRouter", Kind: KindOpenAICompatible,
		Description: "Hundreds of models through one OpenRouter API key.",
		SecretName:  "OPENROUTER_API_KEY", KeyRequired: true,
		BaseURL:         "https://openrouter.ai/api/v1",
		SuggestedModels: []string{},
	},
	{
		ID: "ollama", Name: "Ollama (local models)", Kind: KindOpenAICompatible,
		Description:     "Free, private models running on this PC with Ollama.",
		BaseURL:         "http://127.0.0.1:11434/v1",
		BaseURLEditable: true,
		DefaultModel:    "llama3.3",
		SuggestedModels: []string{"llama3.3", "qwen3", "mistral"},
	},
	{
		ID: "custom", Name: "Custom (OpenAI-compatible)", Kind: KindOpenAICompatible,
		Description:     "Any server that speaks the OpenAI Chat Completions API (LM Studio, vLLM, Groq, Together, ...).",
		SecretName:      "CUSTOM_LLM_API_KEY",
		BaseURLEditable: true,
		SuggestedModels: []string{},
	},
	{
		ID: "demo", Name: "Demo (offline)", Kind: KindDemo,
		Description:     "Sample answers with no model at all. Good for trying Cubicle out.",
		SuggestedModels: []string{"demo"},
		DefaultModel:    "demo",
	},
}

// All returns a copy of the catalog.
func All() []Provider {
	out := make([]Provider, len(catalog))
	copy(out, catalog)
	return out
}

func Get(id string) (Provider, bool) {
	for _, p := range catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}
