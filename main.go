package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// Hilfsfunktion: Fehler als JSON senden
func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp, _ := json.Marshal(map[string]string{"error": msg})
	w.Write(resp)
}

// Hilfsfunktion: Beliebiges JSON senden
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp, _ := json.Marshal(payload)
	w.Write(resp)
}

// Ersetzt "böse" Wörter durch **** (case-insensitive, nur exakte Wortübereinstimmung)
func cleanProfanity(text string) string {
	badWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}
	words := strings.Split(text, " ")
	for i, word := range words {
		lower := strings.ToLower(word)
		if _, found := badWords[lower]; found {
			words[i] = "****"
		}
	}
	return strings.Join(words, " ")
}

// Handler für /api/validate_chirp: Prüft die Länge des Chirps und gibt das Ergebnis als JSON zurück.
func handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	type requestBody struct {
		Body string `json:"body"`
	}
	type errorResponse struct {
		Error string `json:"error"`
	}
	type cleanedResponse struct {
		CleanedBody string `json:"cleaned_body"`
	}

	var req requestBody
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&req)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if len(req.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleaned := cleanProfanity(req.Body)
	respondWithJSON(w, http.StatusOK, cleanedResponse{CleanedBody: cleaned})
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

	// Chirp-Validierungs-Endpunkt (nur POST, JSON)
	mux.HandleFunc("/api/validate_chirp", handlerValidateChirp)

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
- /api/validate_chirp (POST): Erwartet JSON {"body": "..."} und prüft, ob der Text <= 140 Zeichen ist.
  - Bei Erfolg: {"cleaned_body":"..."} (böse Wörter werden durch **** ersetzt)
  - Bei Fehler: {"error":"Chirp is too long"} oder {"error":"Something went wrong"}
- Die Middleware zählt alle Zugriffe auf /app/.
- Die Zählvariable ist threadsicher durch atomic.Int32.
*/
