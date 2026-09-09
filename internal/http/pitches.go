package http

import (
	"errors"
	"net/http"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/pitches"
)

type pitchListPageData struct {
	viewData
	Title       string
	ActiveNav   string
	Heading     string
	Description string
	Pitches     []pitches.Pitch
	Error       string
}

type pitchFormPageData struct {
	viewData
	Title       string
	ActiveNav   string
	Heading     string
	Description string
	Code        string
	Label       string
	Error       string
}

func (r *Router) pitchList(w http.ResponseWriter, req *http.Request) {
	items, err := r.pitches.ListAll(req.Context())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	r.render(w, "pitches.html", pitchListPageData{
		viewData:    r.viewData(req),
		Title:       "Emplacements",
		ActiveNav:   "pitches",
		Heading:     "Emplacements",
		Description: "Gestion des emplacements.",
		Pitches:     items,
		Error:       req.URL.Query().Get("error"),
	})
}

func (r *Router) pitchNew(w http.ResponseWriter, req *http.Request) {
	r.renderPitchForm(w, pitchFormPageData{
		viewData:    r.viewData(req),
		Title:       "Nouvel emplacement",
		ActiveNav:   "pitches",
		Heading:     "Nouvel emplacement",
		Description: "Créer un emplacement qui pourra recevoir des tickets WiFi.",
	})
}

func (r *Router) pitchCreate(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	input := pitches.PitchCreateInput{
		Code:  req.PostFormValue("code"),
		Label: req.PostFormValue("label"),
	}
	if _, err := r.pitches.Create(req.Context(), input); err != nil {
		data := pitchFormPageData{
			viewData:    r.viewData(req),
			Title:       "Nouvel emplacement",
			ActiveNav:   "pitches",
			Heading:     "Nouvel emplacement",
			Description: "Créer un emplacement qui pourra recevoir des tickets WiFi.",
			Code:        input.Code,
			Label:       input.Label,
			Error:       pitchCreateError(err),
		}
		status := http.StatusBadRequest
		if data.Error == "" {
			data.Error = "L'emplacement n'a pas pu être créé."
			status = http.StatusInternalServerError
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		r.renderPitchForm(w, data)
		return
	}

	http.Redirect(w, req, "/pitches", http.StatusSeeOther)
}

func (r *Router) pitchDisable(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		http.NotFound(w, req)
		return
	}

	if _, err := r.pitches.Disable(req.Context(), id); err != nil {
		if errors.Is(err, pitches.ErrPitchNotFound) {
			http.NotFound(w, req)
			return
		}
		http.Redirect(w, req, "/pitches?error=disable", http.StatusSeeOther)
		return
	}

	http.Redirect(w, req, "/pitches", http.StatusSeeOther)
}

func (r *Router) pitchEnable(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		http.NotFound(w, req)
		return
	}

	if _, err := r.pitches.Enable(req.Context(), id); err != nil {
		if errors.Is(err, pitches.ErrPitchNotFound) {
			http.NotFound(w, req)
			return
		}
		http.Redirect(w, req, "/pitches?error=enable", http.StatusSeeOther)
		return
	}

	http.Redirect(w, req, "/pitches", http.StatusSeeOther)
}

func (r *Router) renderPitchForm(w http.ResponseWriter, data pitchFormPageData) {
	r.render(w, "pitch_new.html", data)
}

func pitchCreateError(err error) string {
	switch {
	case errors.Is(err, pitches.ErrPitchCodeRequired):
		return "Le code est obligatoire."
	case errors.Is(err, pitches.ErrDuplicateCode):
		return "Ce code d'emplacement existe déjà."
	default:
		return ""
	}
}
