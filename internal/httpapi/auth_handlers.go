package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/geodispatch/supervisor/internal/auth"
	"github.com/geodispatch/supervisor/internal/models"
)

type authHandler struct {
	db     *gorm.DB
	secret string
}

func newAuthHandler(db *gorm.DB, secret string) *authHandler {
	return &authHandler{db: db, secret: secret}
}

// register godoc
// @Summary      Register a new user
// @Description  Creates an operator account. Returns the created user (no token — call /auth/login next).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body body object{email=string,password=string} true "Credentials"
// @Success      201  {object}  object{id=string,email=string,created_at=string}
// @Failure      400  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /auth/register [post]
func (h *authHandler) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if body.Email == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email_and_password_required"})
		return
	}
	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password_too_short", "min": 8})
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		log.Printf("[auth] bcrypt failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	user := models.User{Email: body.Email, Password: hash}
	if err := h.db.Create(&user).Error; err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "email_already_registered"})
			return
		}
		log.Printf("[auth] register db error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         user.ID,
		"email":      user.Email,
		"created_at": user.CreatedAt,
	})
}

// login godoc
// @Summary      Login
// @Description  Validates credentials and returns a signed JWT (24 h TTL). Pass it as "Authorization: Bearer <token>" on /api/ routes.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body body object{email=string,password=string} true "Credentials"
// @Success      200  {object}  object{token=string}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /auth/login [post]
func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))

	var user models.User
	if err := h.db.Where("email = ?", body.Email).First(&user).Error; err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_credentials"})
		return
	}

	if err := auth.CheckPassword(user.Password, body.Password); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_credentials"})
		return
	}

	token, err := auth.IssueToken(h.secret, user.ID.String(), user.Email)
	if err != nil {
		log.Printf("[auth] token sign failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}