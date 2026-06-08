package config

type Provider struct {
	Name      string
	Protocol  string
	Model     string
	APIKey    string
	BaseURL   string
	MaxTokens int
	Thinking  bool
}

func (p Provider) WithModel(model string) Provider {
	p.Model = model
	return p
}
