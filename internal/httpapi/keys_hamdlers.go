package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/geodispatch/supervisor/internal/auth"
	"github.com/geodispatch/supervisor/internal/models"
)

type keysHandler struct {
	db     *gorm.DB
	secret string
}

func newKeysHandler(db *gorm.DB, secret string) *keysHandler {
	return &keysHandler{db: db, secret: secret}
}

// listKeys godoc
// @Summary      List all API keys
// @Description  Returns all API keys in the system. key_hash is never included. Requires JWT auth.
// @Tags         keys
// @Produce      json
// @Success      200  {array}   object{id=string,user_id=string,label=string,created_at=string}
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Router       /api/keys [get]
func (h *keysHandler) list(w http.ResponseWriter, r *http.Request) {
	var keys []models.APIKey
	if err := h.db.Order("created_at DESC").Find(&keys).Error; err != nil {
		log.Printf("[api] GET /api/keys: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	// Never expose key_hash — build a safe response manually.
	type safeKey struct {
		ID        uuid.UUID `json:"id"`
		UserID    uuid.UUID `json:"user_id"`
		Label     string    `json:"label"`
		CreatedAt any       `json:"created_at"`
	}
	out := make([]safeKey, len(keys))
	for i, k := range keys {
		out[i] = safeKey{
			ID:        k.ID,
			UserID:    k.UserID,
			Label:     k.Label,
			CreatedAt: k.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// createKey godoc
// @Summary      Create an API key
// @Description  Generates a new API key for the authenticated user. The raw key is returned once and never stored — save it immediately.
// @Tags         keys
// @Accept       json
// @Produce      json
// @Param        body body object{label=string} false "Optional label"
// @Success      201  {object}  object{id=string,key=string,label=string,created_at=string}
// @Failure      401  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Router       /api/keys [post]
func (h *keysHandler) create(w http.ResponseWriter, r *http.Request) {
	// API key creation requires a JWT — you cannot create a key using a key.
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "jwt_required"})
		return
	}

	var body struct {
		Label string `json:"label"`
	}
	// Body is optional — a missing or empty body is fine.
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	body.Label = strings.TrimSpace(body.Label)

	// Generate a cryptographically random 32-byte (64 hex char) key.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[api] POST /api/keys generate: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	raw := hex.EncodeToString(b)

	hash, err := auth.HashPassword(raw)
	if err != nil {
		log.Printf("[api] POST /api/keys bcrypt: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		log.Printf("[api] POST /api/keys bad uuid in claims: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	key := models.APIKey{
		UserID:  userID,
		KeyHash: hash,
		Label:   body.Label,
	}
	if err := h.db.Create(&key).Error; err != nil {
		log.Printf("[api] POST /api/keys db: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	// Return the raw key exactly once — it is never retrievable again.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         key.ID,
		"key":        raw,
		"label":      key.Label,
		"created_at": key.CreatedAt,
	})
}

// deleteKey godoc
// @Summary      Revoke an API key
// @Description  Permanently deletes an API key by its UUID. Returns 204 on success.
// @Tags         keys
// @Produce      json
// @Param        id  path  string  true  "API Key UUID"
// @Success      204
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/keys/{id} [delete]
func (h *keysHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id_required"})
		return
	}
	// Validate it's a real UUID before hitting the DB.
	if _, err := uuid.Parse(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_id"})
		return
	}

	res := h.db.Where("id = ?", id).Delete(&models.APIKey{})
	if res.Error != nil {
		log.Printf("[api] DELETE /api/keys/%s: %v", id, res.Error)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	if res.RowsAffected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "key_not_found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}