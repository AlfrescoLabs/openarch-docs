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
