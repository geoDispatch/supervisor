package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/geodispatch/supervisor/internal/models"
)

type restHandler struct {
	db *gorm.DB
}

func newRestHandler(db *gorm.DB) *restHandler {
	return &restHandler{db: db}
}

// registerREST mounts the /api/* routes onto mux.
// All routes are wrapped with a.protected() — rate limiting + JWT-or-API-key.
func (a *API) registerREST(mux *http.ServeMux) {
	h := newRestHandler(a.opts.GormDB)

	mux.Handle("GET /api/devices",         a.protected(http.HandlerFunc(h.listDevices)))
	mux.Handle("POST /api/devices",        a.protected(http.HandlerFunc(h.createDevice)))
	mux.Handle("PUT /api/devices/{id}",    a.protected(http.HandlerFunc(h.updateDevice)))
	mux.Handle("DELETE /api/devices/{id}", a.protected(http.HandlerFunc(h.deleteDevice)))
	mux.Handle("GET /api/events",          a.protected(http.HandlerFunc(h.listEvents)))
	mux.Handle("GET /api/events/{id}",     a.protected(http.HandlerFunc(h.getEvent)))
	mux.Handle("GET /api/shelters",        a.protected(http.HandlerFunc(h.listShelters)))
	mux.Handle("GET /api/rescue-flags",    a.protected(http.HandlerFunc(h.listRescueFlags)))
}

// ── devices ──────────────────────────────────────────────────────────────────

// listDevices godoc
// @Summary      List devices
// @Description  Returns all registered SIM/phone devices ordered by id.
// @Tags         devices
// @Produce      json
// @Success      200  {array}   models.DeviceRow
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/devices [get]
func (h *restHandler) listDevices(w http.ResponseWriter, r *http.Request) {
	var rows []models.DeviceRow
	if err := h.db.Order("id").Find(&rows).Error; err != nil {
		log.Printf("[api] GET /api/devices: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// createDevice godoc
// @Summary      Register a device
// @Description  Registers a new SIM/phone number (E.164 format).
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        body body object{phone=string} true "Device"
// @Success      201  {object}  models.DeviceRow
// @Failure      400  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/devices [post]
func (h *restHandler) createDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Phone == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "phone_required"})
		return
	}
	row := models.DeviceRow{Phone: body.Phone}
	if err := h.db.Create(&row).Error; err != nil {
		if isUniqueErr(err) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "device_already_registered"})
			return
		}
		log.Printf("[api] POST /api/devices: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// updateDevice godoc
// @Summary      Update a device
// @Description  Updates the phone number of a registered device.
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        id   path      int    true  "Device ID"
// @Param        body body      object{phone=string} true "Fields to update"
// @Success      200  {object}  models.DeviceRow
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/devices/{id} [put]
func (h *restHandler) updateDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := intID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Phone *string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	updates := map[string]any{}
	if body.Phone != nil {
		updates["phone"] = *body.Phone
	}
	if len(updates) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nothing_to_update"})
		return
	}
	res := h.db.Model(&models.DeviceRow{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		if isUniqueErr(res.Error) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "phone_already_exists"})
			return
		}
		log.Printf("[api] PUT /api/devices/%d: %v", id, res.Error)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	if res.RowsAffected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "device_not_found"})
		return
	}
	var row models.DeviceRow
	h.db.First(&row, id)
	writeJSON(w, http.StatusOK, row)
}

// deleteDevice godoc
// @Summary      Delete a device
// @Description  Removes a device by its integer id. Returns 204 on success.
// @Tags         devices
// @Produce      json
// @Param        id  path  int  true  "Device ID"
// @Success      204
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/devices/{id} [delete]
func (h *restHandler) deleteDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := intID(w, r, "id")
	if !ok {
		return
	}
	res := h.db.Delete(&models.DeviceRow{}, id)
	if res.Error != nil {
		log.Printf("[api] DELETE /api/devices/%d: %v", id, res.Error)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	if res.RowsAffected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "device_not_found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── events ───────────────────────────────────────────────────────────────────

// listEvents godoc
// @Summary      List events
// @Description  Returns all disaster events, newest first.
// @Tags         events
// @Produce      json
// @Success      200  {array}   models.EventRow
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/events [get]
func (h *restHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	var rows []models.EventRow
	if err := h.db.Order("created_at DESC").Find(&rows).Error; err != nil {
		log.Printf("[api] GET /api/events: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// getEvent godoc
// @Summary      Get event
// @Description  Returns a single disaster event and all its device logs.
// @Tags         events
// @Produce      json
// @Param        id  path      string  true  "Event ID (from USGS/sensor source)"
// @Success      200  {object}  object{event=models.EventRow,logs=[]models.DeviceLogRow}
// @Failure      404  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/events/{id} [get]
func (h *restHandler) getEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id_required"})
		return
	}
	var event models.EventRow
	if err := h.db.First(&event, "id = ?", id).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "event_not_found"})
		return
	}
	var logs []models.DeviceLogRow
	h.db.Where("event_id = ?", id).Order("logged_at DESC").Find(&logs)
	writeJSON(w, http.StatusOK, map[string]any{
		"event": event,
		"logs":  logs,
	})
}

// ── shelters ─────────────────────────────────────────────────────────────────

// listShelters godoc
// @Summary      List shelters
// @Description  Returns all shelters ordered by id. Location (PostGIS) is not included; use a GIS client for spatial queries.
// @Tags         shelters
// @Produce      json
// @Success      200  {array}   models.ShelterRow
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/shelters [get]
func (h *restHandler) listShelters(w http.ResponseWriter, r *http.Request) {
	var rows []models.ShelterRow
	if err := h.db.Order("id").Find(&rows).Error; err != nil {
		log.Printf("[api] GET /api/shelters: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// ── rescue flags ──────────────────────────────────────────────────────────────

// listRescueFlags godoc
// @Summary      Rescue queue
// @Description  Returns all rescue flags ordered by priority descending then by time flagged ascending.
// @Tags         rescue
// @Produce      json
// @Success      200  {array}   models.RescueFlagRow
// @Failure      500  {object}  object{error=string}
// @Security     BearerAuth
// @Security     APIKeyAuth
// @Router       /api/rescue-flags [get]
func (h *restHandler) listRescueFlags(w http.ResponseWriter, r *http.Request) {
	var rows []models.RescueFlagRow
	if err := h.db.Order("rescue_priority DESC, flagged_at ASC").Find(&rows).Error; err != nil {
		log.Printf("[api] GET /api/rescue-flags: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func intID(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	raw := r.PathValue(name)
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_id"})
		return 0, false
	}
	return id, true
}

func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate") || strings.Contains(s, "23505")
}