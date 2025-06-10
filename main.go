package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// apiConfig hält den Zähler für FileServer-Aufrufe.
type apiConfig struct {
	fileserverHits atomic.Int32
}

// Middleware, die bei jedem Aufruf den Zähler erhöht.
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// Handler für /admin/metrics: Gibt die aktuelle Anzahl der Aufrufe als HTML zurück (nur GET).
func (cfg *apiConfig) handlerAdminMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileserverHits.Load())
}

// Handler für /admin/reset: Setzt den Zähler zurück (nur POST).
func (cfg *apiConfig) handlerAdminReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("OK"))
}

func main() {
	mux := http.NewServeMux()
	apiCfg := &apiConfig{}

	// FileServer für das aktuelle Verzeichnis, mit Zähl-Middleware
	fs := http.FileServer(http.Dir("."))
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fs)))

	// Readiness Endpoint für /api/healthz (nur GET)
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Admin-Metrik-Endpunkt (nur GET, HTML)
	mux.HandleFunc("/admin/metrics", apiCfg.handlerAdminMetrics)

	// Admin-Reset-Endpunkt (nur POST)
	mux.HandleFunc("/admin/reset", apiCfg.handlerAdminReset)

	// Erstellt und startet den HTTP-Server auf Port 8080 mit dem konfigurierten ServeMux
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	server.ListenAndServe() // Startet den Server (blockierend)
}

/*
Dokumentation:
--------------
- /app/ (FileServer): Statische Dateien, jeder Zugriff erhöht den Zähler.
- /admin/metrics (GET): Gibt die aktuelle Anzahl der Zugriffe als HTML-Seite zurück.
- /admin/reset (POST): Setzt den Zugriffszähler auf 0 zurück.
- /api/healthz (GET): Readiness-Check, gibt "OK" zurück.
- Die Middleware zählt alle Zugriffe auf /app/.
- Die Zählvariable ist threadsicher durch atomic.Int32.
*/
