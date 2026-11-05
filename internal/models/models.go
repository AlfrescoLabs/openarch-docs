// Package models talks to local models on Docker Model Runner through its
// OpenAI-compatible API, and holds the model profiles.
package models

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// DefaultURL is the Docker Model Runner API on the host, with TCP access on.
const DefaultURL = "http://localhost:12434/engines/v1"

// Profile names the models for one kind of computer.
type Profile struct {
	Name string `json:"name"`
	// Agent makes tool calls in the Docker Agent environment.
	Agent string `json:"agent"`
	// Chat writes answers in 'ask'. It needs no tool calls.
	Chat string `json:"chat"`
	// Vision reads images and page images.
	Vision string `json:"vision"`
	// Embed makes the embeddings for search.
	Embed string `json:"embed"`
	// RAM is the memory the profile is made for.
	RAM string `json:"ram"`
}

// Profiles lists the profiles. gemma3 reads images well but cannot make tool
// calls on Docker Model Runner, so it is the vision model only.
var Profiles = []Profile{
	{Name: "small", Agent: "ai/qwen2.5", Chat: "ai/qwen2.5", Vision: "ai/gemma3:4B-Q4_K_M", Embed: "ai/mxbai-embed-large", RAM: "16 GB"},
	{Name: "large", Agent: "ai/glm-4.7-flash:31B", Chat: "ai/glm-4.7-flash:31B", Vision: "ai/gemma3:4B-Q4_K_M", Embed: "ai/mxbai-embed-large", RAM: "32 GB"},
}

// ProfileNames returns the names of the profiles.
func ProfileNames() []string {
	names := make([]string, len(Profiles))
	for i, p := range Profiles {
		names[i] = p.Name
	}
	return names
}

// GetProfile returns the profile with the name.
func GetProfile(name string) (Profile, error) {
	i := slices.IndexFunc(Profiles, func(p Profile) bool { return p.Name == name })
	if i < 0 {
		return Profile{}, fmt.Errorf("unknown profile %q; use %s", name, strings.Join(ProfileNames(), " or "))
	}
	return Profiles[i], nil
}

// Client calls the model API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a client for the API at baseURL.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 10 * time.Minute}}
}

// ErrUnreachable means the API does not answer.
var ErrUnreachable = errors.New("model API not reachable")

// ModelError is an error the API returned for a model, such as a model that is not pulled.
type ModelError struct {
	Model  string
	Status int
	Body   string
}

func (e *ModelError) Error() string {
	return fmt.Sprintf("model %s: HTTP %d: %s", e.Model, e.Status, e.Body)
}

// Embed returns one normalized embedding per input.
func (c *Client) Embed(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	var resp struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := c.post(ctx, "/embeddings", model, map[string]any{"model": model, "input": inputs}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) != len(inputs) {
		return nil, fmt.Errorf("model %s: %d embeddings for %d inputs", model, len(resp.Data), len(inputs))
	}
	out := make([][]float32, len(inputs))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("model %s: embedding index %d out of range", model, d.Index)
		}
		out[d.Index] = Normalize(d.Embedding)
	}
	return out, nil
}

// Message is one chat message. Content is a string, or a []Part for a
// message with images.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// Part is a piece of a message with images: text or an image.
type Part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds an image as a data URL.
type ImageURL struct {
	URL string `json:"url"`
}

// TextPart returns a text part.
func TextPart(text string) Part { return Part{Type: "text", Text: text} }

// ImagePart returns an image part with the image inline, as a data URL.
func ImagePart(mediaType string, data []byte) Part {
	return Part{Type: "image_url", ImageURL: &ImageURL{URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)}}
}

// ChatOptions tune a chat call.
type ChatOptions struct {
	Temperature float64
	MaxTokens   int
	// Think lets models that think before they answer, such as GLM 4.7
	// Flash, do so. When false the chat template is asked to skip it: the
	// thinking can use every token before the answer starts.
	Think bool
}

// ErrNoAnswer means the model used all its tokens before it wrote an answer.
var ErrNoAnswer = errors.New("the model used all its tokens before it wrote an answer")

// Unload asks Docker Model Runner to free the memory of the models. Errors
// are ignored: a model that is not loaded is fine.
func Unload(ctx context.Context, names ...string) {
	for _, n := range names {
		_ = exec.CommandContext(ctx, "docker", "model", "unload", n).Run()
	}
}

// Chat returns the answer of the model to messages.
func (c *Client) Chat(ctx context.Context, model string, messages []Message, opts ChatOptions) (string, error) {
	body := map[string]any{"model": model, "messages": messages, "temperature": opts.Temperature}
	if opts.MaxTokens > 0 {
		body["max_tokens"] = opts.MaxTokens
	}
	if !opts.Think {
		// llama.cpp passes these to the chat template; templates without the
		// variable ignore it.
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	var resp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := c.post(ctx, "/chat/completions", model, body, &resp); err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("model %s: no answer", model)
	}
	choice := resp.Choices[0]
	content := strings.TrimSpace(choice.Message.Content)
	if content == "" && choice.FinishReason == "length" {
		return "", fmt.Errorf("model %s: %w", model, ErrNoAnswer)
	}
	return content, nil
}

func (c *Client) post(ctx context.Context, path, model string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w at %s: %w", ErrUnreachable, c.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &ModelError{Model: model, Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return json.Unmarshal(b, out)
}

// Normalize scales v to length 1, so the dot product of two vectors is their cosine.
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	n := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * n
	}
	return out
}
