package compression

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/m4vic/one-context/internal/compiler"
	"github.com/m4vic/one-context/internal/state"
)

func TestEnhanceOffKeepsDeterministicSummary(t *testing.T) {
	t.Setenv("ONE_CONTEXT_LLM", "off")
	snapshot := compiler.Snapshot{Summary: "baseline", Compression: "deterministic"}
	if err := Enhance(context.Background(), &snapshot, state.LLMConfig{}); err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary != "baseline" {
		t.Fatalf("summary changed: %q", snapshot.Summary)
	}
}

func TestEnhanceWithOllama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "## Current state\nReady"})
	}))
	defer server.Close()
	t.Setenv("ONE_CONTEXT_LLM", "ollama")
	t.Setenv("ONE_CONTEXT_OLLAMA_URL", server.URL)
	t.Setenv("ONE_CONTEXT_MODEL", "test-model")
	snapshot := compiler.Snapshot{Project: "demo", Compression: "deterministic", Fingerprint: "stable"}
	if err := Enhance(context.Background(), &snapshot, state.LLMConfig{}); err != nil {
		t.Fatal(err)
	}
	if snapshot.Compression != "ollama:test-model" || snapshot.LLMSummary == "" || snapshot.Fingerprint != "stable" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestEnhanceRejectsUnsupportedProvider(t *testing.T) {
	t.Setenv("ONE_CONTEXT_LLM", "mystery")
	if err := Enhance(context.Background(), &compiler.Snapshot{}, state.LLMConfig{}); err == nil {
		t.Fatal("expected provider error")
	}
}

func TestEnhanceFailurePreservesBaseline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline API_KEY=do-not-persist", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("ONE_CONTEXT_LLM", "ollama")
	t.Setenv("ONE_CONTEXT_OLLAMA_URL", server.URL)
	snapshot := compiler.Snapshot{Summary: "deterministic baseline", Compression: "deterministic"}
	err := Enhance(context.Background(), &snapshot, state.LLMConfig{})
	if err == nil {
		t.Fatal("expected provider failure")
	}
	if strings.Contains(err.Error(), "do-not-persist") {
		t.Fatalf("provider response leaked into error: %v", err)
	}
	if snapshot.Summary != "deterministic baseline" || snapshot.Compression != "deterministic" {
		t.Fatalf("fallback was changed: %#v", snapshot)
	}
}

func TestBuildPromptRedactsCredentialsAndUsesNewlines(t *testing.T) {
	prompt := buildPrompt(compiler.Snapshot{Project: "demo", Handoff: &state.Handoff{Message: "API_KEY=super-secret-value"}, DiffExcerpt: "token: ghp_abcdefghijklmnopqrstuvwxyz"})
	if strings.Contains(prompt, "super-secret-value") || strings.Contains(prompt, "ghp_abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("prompt leaked a credential: %s", prompt)
	}
	if strings.Contains(prompt, `\\n`) || !strings.Contains(prompt, "Project: demo\nBranch:") {
		t.Fatalf("prompt does not contain real newlines: %q", prompt)
	}
}

func TestEvaluationCorpusPromptSafety(t *testing.T) {
	data, err := os.ReadFile("testdata/evaluation_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name         string   `json:"name"`
		Project      string   `json:"project"`
		Handoff      string   `json:"handoff"`
		DiffExcerpt  string   `json:"diff_excerpt"`
		ExpectPrompt []string `json:"expect_prompt"`
		ForbidPrompt []string `json:"forbid_prompt"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			prompt := buildPrompt(compiler.Snapshot{Project: testCase.Project, Handoff: &state.Handoff{Message: testCase.Handoff}, DiffExcerpt: testCase.DiffExcerpt})
			for _, expected := range testCase.ExpectPrompt {
				if !strings.Contains(prompt, expected) {
					t.Fatalf("prompt missing %q: %s", expected, prompt)
				}
			}
			for _, forbidden := range testCase.ForbidPrompt {
				if strings.Contains(prompt, forbidden) {
					t.Fatalf("prompt leaked %q: %s", forbidden, prompt)
				}
			}
		})
	}
}

func TestEnhanceWithOpenAICompatibleAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing bearer token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "## Current state\nAPI summary"}}}})
	}))
	defer server.Close()
	t.Setenv("ONE_CONTEXT_LLM", "openai")
	t.Setenv("ONE_CONTEXT_API_URL", server.URL+"/v1")
	t.Setenv("ONE_CONTEXT_API_KEY", "secret")
	t.Setenv("ONE_CONTEXT_MODEL", "small-model")
	snapshot := compiler.Snapshot{Project: "demo", Summary: "baseline", Compression: "deterministic"}
	if err := Enhance(context.Background(), &snapshot, state.LLMConfig{}); err != nil {
		t.Fatal(err)
	}
	if snapshot.Compression != "openai:small-model" || snapshot.LLMSummary == "" || snapshot.Summary != "baseline" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestEnhanceWithClaude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "secret" || r.Header.Get("anthropic-version") == "" {
			t.Fatal("invalid Claude request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]string{"type": "text", "text": "## Current state\nClaude summary"}}})
	}))
	defer server.Close()
	t.Setenv("ONE_CONTEXT_LLM", "claude")
	t.Setenv("ONE_CONTEXT_MODEL", "claude-test")
	t.Setenv("ONE_CONTEXT_ANTHROPIC_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	snapshot := compiler.Snapshot{Project: "demo"}
	if err := Enhance(context.Background(), &snapshot, state.LLMConfig{}); err != nil {
		t.Fatal(err)
	}
	if snapshot.LLMSummary == "" || snapshot.Compression != "claude:claude-test" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestEnhanceWithGemini(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-test:generateContent" || r.Header.Get("x-goog-api-key") != "secret" {
			t.Fatal("invalid Gemini request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]string{"text": "## Current state\nGemini summary"}}}}}})
	}))
	defer server.Close()
	t.Setenv("ONE_CONTEXT_LLM", "gemini")
	t.Setenv("ONE_CONTEXT_MODEL", "gemini-test")
	t.Setenv("ONE_CONTEXT_GEMINI_URL", server.URL)
	t.Setenv("GEMINI_API_KEY", "secret")
	snapshot := compiler.Snapshot{Project: "demo"}
	if err := Enhance(context.Background(), &snapshot, state.LLMConfig{}); err != nil {
		t.Fatal(err)
	}
	if snapshot.LLMSummary == "" || snapshot.Compression != "gemini:gemini-test" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}
