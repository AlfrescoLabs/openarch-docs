package models

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if r.URL.Path != "/engines/v1/embeddings" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "ai/mxbai-embed-large" {
			http.Error(w, "model not found", http.StatusNotFound)
			return
		}
		// Answer out of order, as the API may.
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0,2]},{"index":0,"embedding":[3,4]}]}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL + "/engines/v1/")
	got, err := c.Embed(context.Background(), "ai/mxbai-embed-large", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(got[0][0])-0.6) > 1e-6 || math.Abs(float64(got[0][1])-0.8) > 1e-6 || got[1][1] != 1 {
		t.Errorf("embeddings = %v", got)
	}
	_, err = c.Embed(context.Background(), "other", []string{"a"})
	var me *ModelError
	if !errors.As(err, &me) || me.Status != http.StatusNotFound || me.Model != "other" {
		t.Errorf("err = %v", err)
	}
}

func TestUnreachable(t *testing.T) {
	_, err := NewClient("http://127.0.0.1:9").Embed(context.Background(), "m", []string{"a"})
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("err = %v", err)
	}
}

func TestProfiles(t *testing.T) {
	for _, name := range ProfileNames() {
		p, err := GetProfile(name)
		if err != nil || p.Agent == "" || p.Chat == "" || p.Vision == "" || p.Embed == "" {
			t.Errorf("%s = %+v, %v", name, p, err)
		}
	}
	if _, err := GetProfile("huge"); err == nil {
		t.Error("huge is not a profile")
	}
}

func TestChat(t *testing.T) {
	var got map[string]any
	answer := `{"choices":[{"finish_reason":"stop","message":{"content":" Hola "}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	text, err := c.Chat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, ChatOptions{MaxTokens: 10})
	if err != nil || text != "Hola" {
		t.Fatalf("Chat = %q, %v", text, err)
	}
	kw, _ := got["chat_template_kwargs"].(map[string]any)
	if kw["enable_thinking"] != false || got["max_tokens"] != float64(10) {
		t.Errorf("request = %v", got)
	}
	if _, err := c.Chat(context.Background(), "m", nil, ChatOptions{Think: true}); err != nil || got["chat_template_kwargs"] != nil {
		t.Errorf("think request = %v, %v", got, err)
	}
	answer = `{"choices":[{"finish_reason":"length","message":{"content":""}}]}`
	if _, err := c.Chat(context.Background(), "m", nil, ChatOptions{}); !errors.Is(err, ErrNoAnswer) {
		t.Errorf("err = %v", err)
	}
}
