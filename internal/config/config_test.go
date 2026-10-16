package config

import "testing"

func TestResolveModel(t *testing.T) {
	cases := []struct{ env, saved, want string }{
		{"qwen2.5:7b", "llama3.2", "qwen2.5:7b"}, // the environment wins
		{"", "llama3.2", "llama3.2"},             // then the saved choice
		{"  ", "", DefaultOllamaModel},           // then the default
	}
	for _, c := range cases {
		if got := ResolveModel(c.env, c.saved); got != c.want {
			t.Errorf("ResolveModel(%q, %q) = %q, want %q", c.env, c.saved, got, c.want)
		}
	}
	if DefaultOllamaModel != "deepseek-v2:latest" {
		t.Errorf("default model = %q", DefaultOllamaModel)
	}
}
