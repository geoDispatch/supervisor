package sensor

import (
	"encoding/json"
	"net/http"
	"github.com/geodispatch/supervisor/internal/models"
)

func Parse(r *http.Request) (*models.SensorInput, error) {
	var input models.SensorInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		return nil, err
	}
	return &input, nil
}