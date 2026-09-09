package http

import (
	"context"
	"html/template"
	"net/http"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/adminauth"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/config"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/database"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/pitches"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/static"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/tickets"
)

type Dependencies struct {
	Config    config.Config
	DB        *database.Handle
	RadiusDB  *database.Handle
	Templates *template.Template
	Tickets   *tickets.Service
	Pitches   *pitches.Service
	AdminAuth *adminauth.Service
}

type Router struct {
	cfg       config.Config
	db        *database.Handle
	radiusDB  *database.Handle
	templates *template.Template
	tickets   *tickets.Service
	pitches   *pitches.Service
	adminAuth *adminauth.Service
}

type viewData struct {
	CurrentAdmin *adminauth.AdminUser
}

type pageData struct {
	viewData
	Title       string
	ActiveNav   string
	Heading     string
	Description string
}

type dashboardPageData struct {
	viewData
	Title         string
	ActiveNav     string
	Heading       string
	Description   string
	CreatedToday  int
	ActiveTickets int
	ExpiringToday int
	RevokedToday  int
	RecentTickets []ticketRow
}

func NewRouter(deps Dependencies) http.Handler {
	router := &Router{
		cfg:       deps.Config,
		db:        deps.DB,
		radiusDB:  deps.RadiusDB,
		templates: deps.Templates,
		tickets:   deps.Tickets,
		pitches:   deps.Pitches,
		adminAuth: deps.AdminAuth,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", router.loginForm)
	mux.HandleFunc("POST /api/admin/auth/login", router.loginSubmit)
	mux.HandleFunc("GET /healthz", router.healthz)
	mux.Handle("GET /", router.RequireAdmin(http.HandlerFunc(router.dashboard)))
	mux.Handle("GET /tickets", router.RequireAdmin(http.HandlerFunc(router.ticketList)))
	mux.Handle("GET /tickets/new", router.RequireAdmin(http.HandlerFunc(router.ticketNew)))
	mux.Handle("GET /tickets/print", router.RequireAdmin(http.HandlerFunc(router.ticketPrintSelect)))
	mux.Handle("GET /tickets/print/view", router.RequireAdmin(http.HandlerFunc(router.ticketPrintView)))
	mux.Handle("GET /tickets/print/receipt", router.RequireAdmin(http.HandlerFunc(router.ticketReceiptView)))
	mux.Handle("POST /tickets", router.RequireAdmin(http.HandlerFunc(router.ticketCreate)))
	mux.Handle("POST /tickets/{id}/revoke", router.RequireAdmin(http.HandlerFunc(router.ticketRevoke)))
	mux.Handle("GET /pitches", router.RequireAdmin(http.HandlerFunc(router.pitchList)))
	mux.Handle("GET /pitches/new", router.RequireAdmin(http.HandlerFunc(router.pitchNew)))
	mux.Handle("POST /pitches", router.RequireAdmin(http.HandlerFunc(router.pitchCreate)))
	mux.Handle("POST /pitches/{id}/disable", router.RequireAdmin(http.HandlerFunc(router.pitchDisable)))
	mux.Handle("POST /pitches/{id}/enable", router.RequireAdmin(http.HandlerFunc(router.pitchEnable)))
	mux.Handle("POST /api/admin/auth/logout", router.RequireAdmin(http.HandlerFunc(router.logoutSubmit)))
	mux.Handle("GET /api/admin/auth/me", router.RequireAdmin(http.HandlerFunc(router.authMe)))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.FS))))

	return mux
}

func (r *Router) dashboard(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}

	allTickets, err := r.tickets.ListAll(req.Context())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	allPitches, err := r.pitches.ListAll(req.Context())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	stats := dashboardStats(allTickets, time.Now())
	recentTickets := buildTicketRows(firstTickets(allTickets, 5), allPitches)

	r.render(w, "dashboard.html", dashboardPageData{
		viewData:      r.viewData(req),
		Title:         "WiFi Admin",
		ActiveNav:     "dashboard",
		Heading:       "Dashboard",
		Description:   "Bienvenue sur le portail d'administration du Wi-Fi.",
		RecentTickets: recentTickets,
		CreatedToday:  stats.createdToday,
		ActiveTickets: stats.activeTickets,
		ExpiringToday: stats.expiringToday,
		RevokedToday:  stats.revokedToday,
	})
}

type dashboardTicketStats struct {
	createdToday  int
	activeTickets int
	expiringToday int
	revokedToday  int
}

func dashboardStats(items []tickets.Ticket, now time.Time) dashboardTicketStats {
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfTomorrow := startOfDay.AddDate(0, 0, 1)
	var stats dashboardTicketStats

	for _, ticket := range items {
		if !ticket.CreatedAt.Before(startOfDay) && ticket.CreatedAt.Before(startOfTomorrow) {
			stats.createdToday++
		}
		if ticket.Status == tickets.TicketStatusActive {
			stats.activeTickets++
			if !ticket.ValidUntil.Before(startOfDay) && ticket.ValidUntil.Before(startOfTomorrow) {
				stats.expiringToday++
			}
		}
		if ticket.RevokedAt != nil && !ticket.RevokedAt.Before(startOfDay) && ticket.RevokedAt.Before(startOfTomorrow) {
			stats.revokedToday++
		}
	}

	return stats
}

func firstTickets(items []tickets.Ticket, limit int) []tickets.Ticket {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func (r *Router) healthz(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
	defer cancel()
	if err := r.db.Ping(ctx); err != nil {
		http.Error(w, "admin database unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := r.radiusDB.Ping(ctx); err != nil {
		http.Error(w, "radius database unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (r *Router) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
