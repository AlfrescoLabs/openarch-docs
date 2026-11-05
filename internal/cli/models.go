package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/AlfrescoLabs/openarch-docs/internal/models"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
)

// profile returns the model profile chosen with --profile.
func (a *App) profile() (models.Profile, error) {
	p, err := models.GetProfile(a.Opts.Profile)
	if err != nil {
		return p, output.Usage(err.Error(), "")
	}
	return p, nil
}

func (a *App) modelClient() *models.Client {
	return models.NewClient(a.Opts.ModelURL)
}

// modelError turns a model API error into an error with a fix.
func modelError(err error, model string) error {
	if errors.Is(err, models.ErrUnreachable) {
		return output.NewError(output.ExitPrerequisite, err.Error(),
			"Start Docker Model Runner with host TCP access: docker desktop enable model-runner --tcp 12434. Or use --no-embed.")
	}
	var me *models.ModelError
	if errors.As(err, &me) && (me.Status == http.StatusNotFound || me.Status == http.StatusBadRequest) {
		return output.NewError(output.ExitPrerequisite, err.Error(), fmt.Sprintf("Pull the model: docker model pull %s", model))
	}
	if errors.As(err, &me) {
		return output.NewError(output.ExitRemote, err.Error(), "")
	}
	return err
}
