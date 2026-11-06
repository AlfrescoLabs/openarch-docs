package ask

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
)

// HistoryFile keeps the conversation of ask --continue, relative to the case
// folder. The folder is hidden, so ingest never takes it for an original.
const HistoryFile = ".ask/history.json"

// keepTurns is the number of turns the history file keeps.
const keepTurns = 20

// LoadHistory reads the conversation of a case folder. No file is no history.
func LoadHistory(root string) ([]Turn, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(HistoryFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var turns []Turn
	if err := json.Unmarshal(data, &turns); err != nil {
		return nil, fmt.Errorf("%s: %w", HistoryFile, err)
	}
	return turns, nil
}

// SaveHistory writes the last turns of the conversation of a case folder.
func SaveHistory(root string, turns []Turn) error {
	p := filepath.Join(root, filepath.FromSlash(HistoryFile))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := casefile.MarshalJSON(turns[max(0, len(turns)-keepTurns):])
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// ClearHistory starts a new conversation.
func ClearHistory(root string) error {
	err := os.Remove(filepath.Join(root, filepath.FromSlash(HistoryFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
