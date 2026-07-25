package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/pitches"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/tickets"
)

type ticketListPageData struct {
	viewData
	Title       string
	ActiveNav   string
	Heading     string
	Description string
	Tickets     []ticketRow
	ResultCount int
	Search      string
	Status      string
	Duration    string
	Created     string
	Error       string
}

type ticketFormPageData struct {
	viewData
	Title              string
	ActiveNav          string
	Heading            string
	Description        string
	Pitches            []pitches.Pitch
	PitchID            string
	DurationHours      int
	CustomDurationDays int
	UseCustomDuration  bool
	Error              string
}

type ticketPrintSelectPageData struct {
	viewData
	Title       string
	ActiveNav   string
	Heading     string
	Description string
	Tickets     []ticketRow
	Error       string
}

type ticketPrintPageData struct {
	Title   string
	Tickets []ticketRow
	Message string
}

type ticketRow struct {
	ID         string
	Username   string
	Password   string
	PitchCode  string
	Status     string
	ValidUntil string
	CreatedAt  string
	CanRevoke  bool
}

func (r *Router) ticketList(w http.ResponseWriter, req *http.Request) {
	filters := parseTicketListFilters(req)
	allTickets, err := r.tickets.ListFiltered(req.Context(), filters.Filters)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	allPitches, err := r.pitches.ListAll(req.Context())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	r.render(w, "tickets.html", ticketListPageData{
		viewData:    r.viewData(req),
		Title:       "Tickets WiFi",
		ActiveNav:   "tickets",
		Heading:     "Tickets WiFi",
		Description: "Gestion des tickets WiFi temporaires.",
		Tickets:     buildTicketRows(allTickets, allPitches),
		ResultCount: len(allTickets),
		Search:      filters.Search,
		Status:      filters.Status,
		Duration:    filters.Duration,
		Created:     filters.Created,
		Error:       req.URL.Query().Get("error"),
	})
}

func (r *Router) ticketNew(w http.ResponseWriter, req *http.Request) {
	r.renderTicketForm(w, req, ticketFormPageData{
		viewData:      r.viewData(req),
		Title:         "Nouveau ticket",
		ActiveNav:     "tickets",
		Heading:       "Nouveau ticket WiFi",
		Description:   "Créer un ticket temporaire pour un emplacement.",
		DurationHours: 24,
	})
}

func (r *Router) ticketPrintSelect(w http.ResponseWriter, req *http.Request) {
	rows, err := r.activeTicketRows(req)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	errorMessage := ""
	if req.URL.Query().Get("error") == "no_selection" {
		errorMessage = "Sélectionnez au moins un ticket actif."
	}

	r.render(w, "ticket_print_select.html", ticketPrintSelectPageData{
		viewData:    r.viewData(req),
		Title:       "Imprimer des tickets",
		ActiveNav:   "tickets",
		Heading:     "Imprimer des tickets",
		Description: "Cochez les tickets actifs à imprimer pour l'accueil.",
		Tickets:     rows,
		Error:       errorMessage,
	})
}

func (r *Router) ticketPrintView(w http.ResponseWriter, req *http.Request) {
	r.renderTicketPrint(w, req, "ticket_print.html", "Tickets WiFi à imprimer")
}

func (r *Router) ticketReceiptView(w http.ResponseWriter, req *http.Request) {
	r.renderTicketPrint(w, req, "ticket_receipt.html", "Tickets WiFi - ticket 80 mm")
}

func (r *Router) renderTicketPrint(w http.ResponseWriter, req *http.Request, templateName string, title string) {
	ids := req.URL.Query()["id"]
	if len(ids) == 0 {
		http.Redirect(w, req, "/tickets/print?error=no_selection", http.StatusSeeOther)
		return
	}

	rows, err := r.activeTicketRows(req)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	selected := selectTicketRowsByID(rows, ids)
	data := ticketPrintPageData{
		Title:   title,
		Tickets: selected,
	}
	if len(selected) == 0 {
		data.Message = "Aucun ticket sélectionné n'est imprimable."
	}

	r.render(w, templateName, data)
}

func (r *Router) ticketCreate(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	pitchID := req.PostFormValue("pitch_id")
	durationHours, customDurationDays, useCustomDuration, err := parseTicketDuration(req)
	if err != nil {
		r.renderTicketCreateError(w, req, ticketFormPageData{
			viewData:           r.viewData(req),
			PitchID:            pitchID,
			DurationHours:      durationHours,
			CustomDurationDays: customDurationDays,
			UseCustomDuration:  useCustomDuration,
			Error:              "La durée personnalisée doit être un nombre de jours positif.",
		}, http.StatusBadRequest)
		return
	}

	pitchCode, err := r.pitchCodeForTicket(req, pitchID)
	if err != nil {
		r.renderTicketCreateError(w, req, ticketFormPageData{
			viewData:           r.viewData(req),
			PitchID:            pitchID,
			DurationHours:      durationHours,
			CustomDurationDays: customDurationDays,
			UseCustomDuration:  useCustomDuration,
			Error:              "Sélectionnez un emplacement actif.",
		}, http.StatusBadRequest)
		return
	}

	validFrom := time.Now()
	input := tickets.TicketCreateInput{
		PitchID:    pitchID,
		PitchCode:  pitchCode,
		ValidFrom:  validFrom,
		ValidUntil: validFrom.Add(time.Duration(durationHours) * time.Hour),
	}
	if _, err := r.tickets.Create(req.Context(), input); err != nil {
		status := http.StatusBadRequest
		message := ticketCreateError(err)
		if message == "" {
			message = "Le ticket n'a pas pu être créé."
			status = http.StatusInternalServerError
		}
		r.renderTicketCreateError(w, req, ticketFormPageData{
			viewData:           r.viewData(req),
			PitchID:            pitchID,
			DurationHours:      durationHours,
			CustomDurationDays: customDurationDays,
			UseCustomDuration:  useCustomDuration,
			Error:              message,
		}, status)
		return
	}

	http.Redirect(w, req, "/tickets", http.StatusSeeOther)
}

func (r *Router) ticketRevoke(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		http.NotFound(w, req)
		return
	}

	if _, err := r.tickets.Revoke(req.Context(), tickets.TicketRevokeInput{ID: id}); err != nil {
		if errors.Is(err, tickets.ErrTicketNotFound) {
			http.NotFound(w, req)
			return
		}
		http.Redirect(w, req, "/tickets?error=revoke", http.StatusSeeOther)
		return
	}

	http.Redirect(w, req, "/tickets", http.StatusSeeOther)
}

func (r *Router) renderTicketCreateError(w http.ResponseWriter, req *http.Request, data ticketFormPageData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	r.renderTicketForm(w, req, data)
}

func (r *Router) renderTicketForm(w http.ResponseWriter, req *http.Request, data ticketFormPageData) {
	activePitches, err := r.pitches.ListActive(req.Context())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if data.Title == "" {
		data.Title = "Nouveau ticket"
	}
	if data.ActiveNav == "" {
		data.ActiveNav = "tickets"
	}
	if data.Heading == "" {
		data.Heading = "Nouveau ticket WiFi"
	}
	if data.Description == "" {
		data.Description = "Créer un ticket temporaire pour un emplacement."
	}
	if data.DurationHours == 0 && !data.UseCustomDuration {
		data.DurationHours = 24
	}
	data.Pitches = activePitches

	r.render(w, "ticket_new.html", data)
}

func parseTicketDuration(req *http.Request) (durationHours int, customDurationDays int, useCustomDuration bool, err error) {
	raw := req.PostFormValue("duration_hours")
	if raw == "custom" {
		useCustomDuration = true
		customDurationDays, err = strconv.Atoi(req.PostFormValue("custom_duration_days"))
		if err != nil || customDurationDays <= 0 {
			return 0, customDurationDays, true, errors.New("invalid custom duration")
		}
		return customDurationDays * 24, customDurationDays, true, nil
	}

	durationHours, err = strconv.Atoi(raw)
	if err != nil || durationHours <= 0 {
		return durationHours, customDurationDays, false, errors.New("invalid duration")
	}
	return durationHours, customDurationDays, false, nil
}

func (r *Router) pitchCodeForTicket(req *http.Request, pitchID string) (string, error) {
	pitchID = strings.TrimSpace(pitchID)
	if pitchID == "" {
		return "", errors.New("pitch_id is required")
	}

	activePitches, err := r.pitches.ListActive(req.Context())
	if err != nil {
		return "", err
	}
	for _, pitch := range activePitches {
		if pitch.ID == pitchID {
			return pitch.Code, nil
		}
	}
	return "", errors.New("active pitch not found")
}

func buildTicketRows(items []tickets.Ticket, allPitches []pitches.Pitch) []ticketRow {
	pitchCodes := make(map[string]string, len(allPitches))
	for _, pitch := range allPitches {
		pitchCodes[pitch.ID] = pitch.Code
	}

	rows := make([]ticketRow, 0, len(items))
	for _, ticket := range items {
		rows = append(rows, ticketRow{
			ID:         ticket.ID,
			Username:   ticket.Username,
			Password:   ticket.CleartextPassword,
			PitchCode:  pitchLabel(ticket.PitchID, pitchCodes),
			Status:     ticketStatusLabel(ticket.Status),
			ValidUntil: ticket.ValidUntil.Format("02/01/2006 15:04"),
			CreatedAt:  ticket.CreatedAt.Format("02/01/2006 15:04"),
			CanRevoke:  ticket.Status == tickets.TicketStatusActive,
		})
	}

	return rows
}

func (r *Router) activeTicketRows(req *http.Request) ([]ticketRow, error) {
	activeTickets, err := r.tickets.ListFiltered(req.Context(), tickets.TicketListFilters{
		Status: tickets.TicketStatusActive,
	})
	if err != nil {
		return nil, err
	}

	allPitches, err := r.pitches.ListAll(req.Context())
	if err != nil {
		return nil, err
	}

	return buildTicketRows(activeTickets, allPitches), nil
}

func selectTicketRowsByID(rows []ticketRow, ids []string) []ticketRow {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = struct{}{}
		}
	}

	selected := make([]ticketRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := wanted[row.ID]; ok {
			selected = append(selected, row)
		}
	}

	return selected
}

func pitchLabel(id string, pitchCodes map[string]string) string {
	if code, ok := pitchCodes[id]; ok {
		return code
	}
	return id
}

func ticketStatusLabel(status tickets.TicketStatus) string {
	switch status {
	case tickets.TicketStatusActive:
		return "Actif"
	case tickets.TicketStatusExpired:
		return "Expire"
	case tickets.TicketStatusRevoked:
		return "Revoque"
	default:
		return string(status)
	}
}

func ticketCreateError(err error) string {
	switch {
	case errors.Is(err, tickets.ErrPitchRequired):
		return "Selectionnez un emplacement."
	case errors.Is(err, tickets.ErrPitchCodeRequired):
		return "Le numéro d'emplacement est requis pour générer l'identifiant."
	case errors.Is(err, tickets.ErrInvalidTicketDates):
		return "Les dates de validité sont incohérentes."
	case errors.Is(err, tickets.ErrDuplicateUsername):
		return "Un identifiant identique existe déjà. Réessayez."
	default:
		return ""
	}
}

type parsedTicketListFilters struct {
	Filters  tickets.TicketListFilters
	Search   string
	Status   string
	Duration string
	Created  string
}

func parseTicketListFilters(req *http.Request) parsedTicketListFilters {
	query := req.URL.Query()
	parsed := parsedTicketListFilters{
		Search:   strings.TrimSpace(query.Get("search")),
		Status:   query.Get("status"),
		Duration: query.Get("duration"),
		Created:  query.Get("created"),
	}
	parsed.Filters.Search = parsed.Search

	switch parsed.Status {
	case string(tickets.TicketStatusActive):
		parsed.Filters.Status = tickets.TicketStatusActive
	case string(tickets.TicketStatusExpired):
		parsed.Filters.Status = tickets.TicketStatusExpired
	case string(tickets.TicketStatusRevoked):
		parsed.Filters.Status = tickets.TicketStatusRevoked
	default:
		parsed.Status = ""
	}

	switch parsed.Duration {
	case "1d":
		parsed.Filters.Duration = 24 * time.Hour
	case "7d":
		parsed.Filters.Duration = 7 * 24 * time.Hour
	case "30d":
		parsed.Filters.Duration = 30 * 24 * time.Hour
	default:
		parsed.Duration = ""
	}

	now := time.Now()
	switch parsed.Created {
	case "today":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		parsed.Filters.CreatedSince = &start
	case "week":
		weekdayOffset := (int(now.Weekday()) + 6) % 7
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -weekdayOffset)
		parsed.Filters.CreatedSince = &start
	case "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		parsed.Filters.CreatedSince = &start
	default:
		parsed.Created = ""
	}

	return parsed
}
